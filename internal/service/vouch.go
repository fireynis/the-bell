package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/google/uuid"
)

const dailyVouchLimit = 3

const (
	// revocationPenaltyPoints and revocationPenaltyDecayDays are the cost of
	// withdrawing an endorsement, per the design doc: "-3 for 30 days when you
	// revoke a vouch (prevents vouch-and-revoke gaming, but small enough that
	// revoking a bad actor is still clearly worth it)".
	//
	// The size is the point. Vouching and revoking in a loop should cost
	// something, while removing a vouch you have come to regret must never cost
	// you the ability to vouch at all — 3 points against the 100-point scale
	// leaves a voucher at the threshold still above it.
	revocationPenaltyPoints    = 3.0
	revocationPenaltyDecayDays = 30
)

// startOfDay returns local midnight for now — the window the daily vouch limit
// counts over. The limit is "3 per day" as a resident of the town experiences a
// day, so the window follows the clock's own location rather than UTC; on a DST
// transition the window is correspondingly 23 or 25 hours long.
func startOfDay(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

// VouchRepository abstracts vouch persistence using domain types.
type VouchRepository interface {
	CreateVouch(ctx context.Context, vouch *domain.Vouch) error
	GetVouchByID(ctx context.Context, id string) (*domain.Vouch, error)
	GetVouchByPair(ctx context.Context, voucherID, voucheeID string) (*domain.Vouch, error)
	CountVouchesByVoucherSince(ctx context.Context, voucherID string, since time.Time) (int64, error)
	ListActiveVouchesByVouchee(ctx context.Context, voucheeID string) ([]*domain.Vouch, error)
	ListActiveVouchesByVoucher(ctx context.Context, voucherID string) ([]*domain.Vouch, error)
	RevokeVouch(ctx context.Context, id string) error
}

// GraphQuerier abstracts trust-graph edge operations.
type GraphQuerier interface {
	AddVouchEdge(ctx context.Context, voucherID, voucheeID string) error
	RemoveVouchEdge(ctx context.Context, voucherID, voucheeID string) error
	HasCyclicVouch(ctx context.Context, voucherID, voucheeID string) (bool, error)
}

// UserGetter retrieves users and updates their roles.
type UserGetter interface {
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
	UpdateUserRole(ctx context.Context, id string, role domain.Role) error
}

// VouchService orchestrates vouch business logic.
type VouchService struct {
	vouches    VouchRepository
	graph      GraphQuerier
	users      UserGetter
	now        func() time.Time
	trustQueue TrustRecalcQueue
	penalties  PenaltyRepository
	logger     *slog.Logger
}

func NewVouchService(vouches VouchRepository, graph GraphQuerier, users UserGetter, clock func() time.Time) *VouchService {
	if clock == nil {
		clock = time.Now
	}
	return &VouchService{
		vouches: vouches,
		graph:   graph,
		users:   users,
		now:     clock,
		logger:  slog.Default(),
	}
}

// SetTrustQueue attaches an optional trust recalculation queue, mirroring
// PostService.SetFeedCache. Deployments without Redis leave it nil and simply
// do not recalculate.
func (s *VouchService) SetTrustQueue(q TrustRecalcQueue) {
	s.trustQueue = q
}

// SetPenaltyRepository attaches the store used to record revocation penalties.
// Left nil, revocation still works and simply carries no cost, the same way an
// absent trust queue degrades rather than failing.
func (s *VouchService) SetPenaltyRepository(p PenaltyRepository) {
	s.penalties = p
}

// applyRevocationPenalty charges the voucher for withdrawing their endorsement.
//
// It writes the penalty row directly rather than going through
// PropagatePenalties, which would spread the cost to the voucher's own vouchers.
// That is right for moderation — the people who endorsed an offender share the
// consequences — and wrong here: revoking is a self-inflicted cost on one
// person, so it stays at hop depth 0 and reaches nobody else.
//
// CreatedAt and DecaysAt come from a single clock reading because
// penaltyDecayDays recovers the window as the difference between them; two
// reads would make a "30 day" penalty decay over 29 or 31.
//
// A revocation that has already happened must not be undone because the penalty
// could not be recorded, so a failure is logged rather than returned.
func (s *VouchService) applyRevocationPenalty(ctx context.Context, voucherID string, now time.Time) {
	if s.penalties == nil {
		return
	}

	id, err := uuid.NewV7()
	if err != nil {
		s.logger.Warn("generating revocation penalty id failed", "voucher_id", voucherID, "error", err)
		return
	}

	decaysAt := now.AddDate(0, 0, revocationPenaltyDecayDays)
	penalty := &domain.TrustPenalty{
		ID:     id.String(),
		UserID: voucherID,
		// No moderation action: nobody moderated anything. The column is
		// nullable precisely so this penalty can exist.
		ModerationActionID: "",
		PenaltyAmount:      revocationPenaltyPoints,
		HopDepth:           0,
		CreatedAt:          now,
		DecaysAt:           &decaysAt,
	}

	if err := s.penalties.CreateTrustPenalty(ctx, penalty); err != nil {
		s.logger.Warn("recording vouch revocation penalty failed", "voucher_id", voucherID, "error", err)
	}
}

// enqueueVouchRecalc asks for both sides of a vouch edge to be recomputed.
//
// The vouchee is the one who actually needs it: CalcVoucherScore counts the
// vouches a user has RECEIVED, so gaining or losing an endorsement moves their
// score and nobody else's. The voucher is enqueued too because the design doc
// names them and a second RPush is cheap — under the current model it is a
// no-op, but it becomes load-bearing the moment the voucher component is
// redefined in terms of vouches given.
//
// A vouch that is already committed must not be undone because the queue is
// unreachable, so this only ever logs.
func (s *VouchService) enqueueVouchRecalc(ctx context.Context, voucherID, voucheeID string) {
	if s.trustQueue == nil {
		return
	}
	for _, userID := range []string{voucheeID, voucherID} {
		if err := s.trustQueue.EnqueueRecalc(ctx, userID); err != nil {
			s.logger.Warn("enqueueing trust recalculation failed", "user_id", userID, "error", err)
		}
	}
}

// Vouch creates a new vouch from voucherID to voucheeID.
// It enforces: no self-vouch, trust >= 60, no duplicate pair, daily limit of 3,
// and no graph cycles. On success it also promotes a pending vouchee to member.
//
// A non-nil vouch with a non-nil error means the vouch persisted but the
// promotion that follows it did not; see the promotion block below.
func (s *VouchService) Vouch(ctx context.Context, voucherID, voucheeID string) (*domain.Vouch, error) {
	if voucherID == voucheeID {
		return nil, fmt.Errorf("%w: cannot vouch for yourself", ErrValidation)
	}

	voucher, err := s.users.GetUserByID(ctx, voucherID)
	if err != nil {
		return nil, fmt.Errorf("looking up voucher: %w", err)
	}

	if !voucher.CanVouch() {
		return nil, fmt.Errorf("%w: voucher does not meet trust requirements", ErrForbidden)
	}

	if _, err := s.users.GetUserByID(ctx, voucheeID); err != nil {
		return nil, fmt.Errorf("looking up vouchee: %w", err)
	}

	existing, err := s.vouches.GetVouchByPair(ctx, voucherID, voucheeID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("checking existing vouch: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("%w: vouch already exists for this pair", ErrValidation)
	}

	now := s.now()
	count, err := s.vouches.CountVouchesByVoucherSince(ctx, voucherID, startOfDay(now))
	if err != nil {
		return nil, fmt.Errorf("counting daily vouches: %w", err)
	}
	if count >= dailyVouchLimit {
		return nil, fmt.Errorf("%w: daily vouch limit (%d) reached", ErrValidation, dailyVouchLimit)
	}

	hasCycle, err := s.graph.HasCyclicVouch(ctx, voucherID, voucheeID)
	if err != nil {
		return nil, fmt.Errorf("checking cycle: %w", err)
	}
	if hasCycle {
		return nil, fmt.Errorf("%w: vouch would create a cycle in the trust graph", ErrValidation)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generating vouch id: %w", err)
	}

	vouch := &domain.Vouch{
		ID:        id.String(),
		VoucherID: voucherID,
		VoucheeID: voucheeID,
		Status:    domain.VouchActive,
		CreatedAt: now,
	}

	if err := s.vouches.CreateVouch(ctx, vouch); err != nil {
		return nil, fmt.Errorf("creating vouch: %w", err)
	}

	if err := s.graph.AddVouchEdge(ctx, voucherID, voucheeID); err != nil {
		return nil, fmt.Errorf("adding graph edge: %w", err)
	}

	s.enqueueVouchRecalc(ctx, voucherID, voucheeID)

	// Promote pending users to member on first vouch received.
	//
	// The vouch and its graph edge have already committed and are not rolled
	// back, but a failure here must still reach the caller. This is the primary
	// way people join after bootstrap, and a vouch that reports success while
	// leaving the vouchee pending is unrecoverable from the outside: the pair
	// now has a vouch, so vouching again is rejected as a duplicate, and the
	// voucher has spent one of their three for the day on nothing. Silence
	// turns a retryable write failure into a person who cannot join.
	//
	// The returned error says the promotion did not happen, not that the vouch
	// was undone — the same contract as ApprovalService.Approve's bootstrap
	// exit check, and the vouch is returned alongside it so a caller that wants
	// to report what did persist can.
	//
	// Both failures deliberately drop the sentinel (%v, not %w). The sentinels'
	// only consumer is the handler's status mapping, and a post-commit failure
	// is not the caller's mistake: an ErrNotFound reaching it here would render
	// as 404 "not found", telling the voucher their vouchee does not exist and
	// implying nothing was written, when a vouch was. Unwrapped, these land in
	// the default branch as 500, which is what a half-finished write is.
	vouchee, err := s.users.GetUserByID(ctx, voucheeID)
	if err != nil {
		return vouch, fmt.Errorf("vouch recorded but looking up vouchee for promotion: %v", err)
	}
	if vouchee.Role == domain.RolePending {
		if err := s.users.UpdateUserRole(ctx, voucheeID, domain.RoleMember); err != nil {
			return vouch, fmt.Errorf("vouch recorded but promoting vouchee to member: %v", err)
		}
	}

	return vouch, nil
}

// ListReceivedVouches returns active vouches received by the given user.
func (s *VouchService) ListReceivedVouches(ctx context.Context, userID string) ([]*domain.Vouch, error) {
	return s.vouches.ListActiveVouchesByVouchee(ctx, userID)
}

// ListGivenVouches returns active vouches given by the given user.
func (s *VouchService) ListGivenVouches(ctx context.Context, userID string) ([]*domain.Vouch, error) {
	return s.vouches.ListActiveVouchesByVoucher(ctx, userID)
}

// Revoke revokes an existing vouch. Only the original voucher, a moderator,
// or a council member can revoke.
func (s *VouchService) Revoke(ctx context.Context, vouchID, actorID string) error {
	vouch, err := s.vouches.GetVouchByID(ctx, vouchID)
	if err != nil {
		return fmt.Errorf("looking up vouch: %w", err)
	}

	if vouch.Status == domain.VouchRevoked {
		return fmt.Errorf("%w: vouch is already revoked", ErrValidation)
	}

	actor, err := s.users.GetUserByID(ctx, actorID)
	if err != nil {
		return fmt.Errorf("looking up actor: %w", err)
	}

	if vouch.VoucherID != actorID && !actor.CanModerate() {
		return ErrForbidden
	}

	if err := s.vouches.RevokeVouch(ctx, vouchID); err != nil {
		return fmt.Errorf("revoking vouch: %w", err)
	}

	if err := s.graph.RemoveVouchEdge(ctx, vouch.VoucherID, vouch.VoucheeID); err != nil {
		return fmt.Errorf("removing graph edge: %w", err)
	}

	// Only a voucher withdrawing their own endorsement pays. The penalty exists
	// to make vouch-and-revoke gaming cost something, and the gamer is the
	// voucher; a moderator or council member revoking someone else's vouch is
	// doing the job, and taxing them for it would discourage exactly the
	// clean-up the trust graph depends on. Their instrument for punishing a bad
	// voucher is a moderation action, which carries its own penalty.
	//
	// This is also why enqueueVouchRecalc does not need the actor: the party
	// charged here is always the voucher, whom it already enqueues.
	if vouch.VoucherID == actorID {
		s.applyRevocationPenalty(ctx, vouch.VoucherID, s.now())
	}

	s.enqueueVouchRecalc(ctx, vouch.VoucherID, vouch.VoucheeID)

	return nil
}

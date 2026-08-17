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
	ReactivateVouch(ctx context.Context, id string, createdAt time.Time) error
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
	vouches     VouchRepository
	graph       GraphQuerier
	users       UserGetter
	now         func() time.Time
	trustQueue  TrustRecalcQueue
	penalties   PenaltyRepository
	roleHistory RoleHistoryWriter
	invites     InviteCounter
	logger      *slog.Logger
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

// SetInviteCounter makes the daily allowance count invitations as well as
// vouches, in both directions.
//
// InviteService.Create already counts vouches when it charges the budget. Without
// the mirror image here the allowance would only be combined from one side: a
// member could send three invitations, be refused a fourth, and then give three
// vouches — six endorsements in a day from a rule that says three. An invitation
// is a vouch made in advance, so it has to cost the same thing whichever order
// the two are spent in.
//
// Optional, like the other collaborators here: unset, the limit counts vouches
// alone, which is the behaviour that predates invitations.
func (s *VouchService) SetInviteCounter(c InviteCounter) {
	s.invites = c
}

// InviteCounter reports how many invitations a member has created since a given
// time — the other half of the shared daily allowance.
type InviteCounter interface {
	CountInvitesByInviterSince(ctx context.Context, inviterID string, since time.Time) (int64, error)
}

// endorsementsToday is what the daily allowance is spent on: vouches given plus
// invitations sent, since local midnight.
func (s *VouchService) endorsementsToday(ctx context.Context, voucherID string, since time.Time) (int64, error) {
	count, err := s.vouches.CountVouchesByVoucherSince(ctx, voucherID, since)
	if err != nil {
		return 0, fmt.Errorf("counting daily vouches: %w", err)
	}
	if s.invites == nil {
		return count, nil
	}
	invites, err := s.invites.CountInvitesByInviterSince(ctx, voucherID, since)
	if err != nil {
		return 0, fmt.Errorf("counting daily invites: %w", err)
	}
	return count + invites, nil
}

// SetRoleHistory attaches the audit trail that every role change in this
// codebase writes to.
//
// It is a setter rather than a constructor parameter to match the other
// optional collaborators here, but it is not optional in the way they are: a
// vouch promoting somebody to member is a role change, and until this was wired
// it was the one role change that left no trace — the role checker and council
// votes both recorded theirs, so role_history read as though nobody had ever
// joined by being vouched for. app.Build always sets it, and promoting with no
// writer attached logs a warning rather than passing silently.
func (s *VouchService) SetRoleHistory(w RoleHistoryWriter) {
	s.roleHistory = w
}

// recordPromotion writes the role_history entry for a promotion that has
// already been applied.
//
// The reason names the mechanism and the specific vouch, following
// ProposalService.changeRole: the column is read in a list next to automatic
// promotions and demotions, so it has to say in a few words why this one
// happened and carry the id of the thing that caused it.
func (s *VouchService) recordPromotion(ctx context.Context, userID, vouchID string, oldRole domain.Role) error {
	if s.roleHistory == nil {
		s.logger.Warn("promotion not recorded in role history: no writer wired",
			"user_id", userID, "vouch_id", vouchID)
		return nil
	}

	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generating role history id: %w", err)
	}
	entry := &domain.RoleHistory{
		ID:        id.String(),
		UserID:    userID,
		OldRole:   oldRole,
		NewRole:   domain.RoleMember,
		Reason:    fmt.Sprintf("vouched for: vouch %s", vouchID),
		CreatedAt: s.now(),
	}
	if err := s.roleHistory.CreateRoleHistoryEntry(ctx, entry); err != nil {
		return fmt.Errorf("recording role history: %w", err)
	}
	return nil
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

// Vouch records a vouch from voucherID to voucheeID.
// It enforces: no self-vouch, trust >= 60, no live vouch for the pair, daily
// limit of 3, and no graph cycles. On success it also promotes a pending
// vouchee to member.
//
// A pair whose vouch was revoked can vouch again. The row is reactivated rather
// than replaced, because UNIQUE(voucher_id, vouchee_id) means a second row for
// the pair cannot exist — before this, a revoked row matched the duplicate
// check and made the rejection permanent, while the revoke dialog told the
// member they could vouch again later. Reactivation runs through every gate a
// first vouch does: the trust floor, the daily limit, cycle detection, and the
// pending-vouchee promotion below. The only difference is which write it ends
// in.
//
// Re-vouching does NOT refund the revocation penalty the voucher paid. The
// penalty prices the act of flip-flopping, not the state of the graph — which
// is exactly what vouching again is — and the daily limit and the penalty
// together are what make a vouch/revoke loop cost something. Refunding it would
// make the loop free.
//
// A non-nil vouch with a non-nil error means the vouch persisted but the
// promotion that follows it did not; see the promotion block below.
func (s *VouchService) Vouch(ctx context.Context, voucherID, voucheeID string) (*domain.Vouch, error) {
	return s.vouch(ctx, voucherID, voucheeID, false)
}

// VouchFromInvite records the vouch an accepted invitation always was.
//
// It is Vouch with the daily limit skipped, and only the daily limit. The trust
// floor, the self-vouch and duplicate-pair checks, cycle detection, the graph
// edge, the recalculation enqueue and the promotion all still apply — the
// inviter is re-read and re-tested at this moment, so an inviter who has since
// been suspended or fallen below the threshold cannot carry anybody in on an
// invitation they are no longer entitled to send.
//
// The limit is skipped because it was already spent. Creating the invitation
// charged the member's combined invite-and-vouch budget for that day (see
// InviteService.Create), so counting it again when the invitee finally clicks
// the link would charge them twice for one endorsement — and would do it on a
// day they did not choose, since the invitee decides when to accept. A member
// who sent their three invitations on Monday would find themselves unable to
// vouch on Thursday because three strangers happened to sign up that morning.
func (s *VouchService) VouchFromInvite(ctx context.Context, voucherID, voucheeID string) (*domain.Vouch, error) {
	return s.vouch(ctx, voucherID, voucheeID, true)
}

func (s *VouchService) vouch(ctx context.Context, voucherID, voucheeID string, skipDailyLimit bool) (*domain.Vouch, error) {
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
	// Only a revoked vouch may be given again. Anything else that already
	// exists is a live endorsement, and endorsing someone twice is the
	// duplicate this has always refused. Testing for "revoked" rather than
	// "not active" is the fail-safe direction: a status added later is treated
	// as live until somebody decides otherwise.
	if existing != nil && existing.Status != domain.VouchRevoked {
		return nil, fmt.Errorf("%w: vouch already exists for this pair", ErrValidation)
	}

	now := s.now()
	// Council is exempt from the daily allowance, for the reason spelled out in
	// InviteService.Create: it mirrors the deliberate decision to leave council
	// approvals unlimited, and in an invite-only town the council's endorsements
	// are how the town gets populated at all. The check is here as well as
	// there so the exemption cannot be spent from one side only.
	if !skipDailyLimit && !voucher.IsCouncil() {
		count, err := s.endorsementsToday(ctx, voucherID, startOfDay(now))
		if err != nil {
			return nil, err
		}
		if count >= dailyVouchLimit {
			return nil, fmt.Errorf("%w: daily limit (%d) reached; invites and vouches share one allowance",
				ErrValidation, dailyVouchLimit)
		}
	}

	hasCycle, err := s.graph.HasCyclicVouch(ctx, voucherID, voucheeID)
	if err != nil {
		return nil, fmt.Errorf("checking cycle: %w", err)
	}
	if hasCycle {
		return nil, fmt.Errorf("%w: vouch would create a cycle in the trust graph", ErrValidation)
	}

	vouch := &domain.Vouch{
		VoucherID: voucherID,
		VoucheeID: voucheeID,
		Status:    domain.VouchActive,
		CreatedAt: now,
	}

	if existing != nil {
		vouch.ID = existing.ID
		if err := s.vouches.ReactivateVouch(ctx, existing.ID, now); err != nil {
			// The UPDATE matching nothing means the row is no longer revoked:
			// a concurrent request for the same pair reactivated it between
			// the read above and this write. That is the create path's race
			// seen from the other side — there, the unique constraint fires
			// and the adapter maps it to a duplicate — so it gets the same
			// answer, rather than a 404 telling the voucher a vouch they can
			// see does not exist.
			if errors.Is(err, ErrNotFound) {
				return nil, fmt.Errorf("%w: vouch already exists for this pair", ErrValidation)
			}
			return nil, fmt.Errorf("reactivating vouch: %w", err)
		}
	} else {
		id, err := uuid.NewV7()
		if err != nil {
			return nil, fmt.Errorf("generating vouch id: %w", err)
		}
		vouch.ID = id.String()
		if err := s.vouches.CreateVouch(ctx, vouch); err != nil {
			return nil, fmt.Errorf("creating vouch: %w", err)
		}
	}

	// Restores the edge Revoke deleted, on the reactivation path. AddVouchEdge
	// MERGEs, so it is the same call either way — the graph and the table stay
	// in agreement without this branch having to know which one it is on.
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
		// Captured before the write, for the reason ProposalService.changeRole
		// spells out: old_role is a fact about the moment before the update,
		// and reading it off the user afterwards would depend on the repository
		// not having touched the value in hand. A store that does — any
		// in-memory one — silently turns every entry into member -> member.
		oldRole := vouchee.Role
		if err := s.users.UpdateUserRole(ctx, voucheeID, domain.RoleMember); err != nil {
			return vouch, fmt.Errorf("vouch recorded but promoting vouchee to member: %v", err)
		}
		// The audit entry follows the same post-commit contract as the
		// promotion above and for the same reason: the role has changed and is
		// not being rolled back, but a promotion missing from role_history is a
		// person whose membership has no recorded origin, which is precisely
		// what the trail exists to prevent. Unwrapped (%v) so it lands as a 500
		// rather than being mistaken for the caller's error.
		if err := s.recordPromotion(ctx, voucheeID, vouch.ID, oldRole); err != nil {
			return vouch, fmt.Errorf("vouch recorded and vouchee promoted but %v", err)
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

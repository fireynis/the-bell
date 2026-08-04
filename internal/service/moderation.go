package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/google/uuid"
)

// PenaltyRepository abstracts trust penalty persistence using domain types.
type PenaltyRepository interface {
	CreateTrustPenalty(ctx context.Context, penalty *domain.TrustPenalty) error
}

// PenaltyGraphQuerier abstracts graph traversal for penalty propagation.
type PenaltyGraphQuerier interface {
	FindVouchersWithDepth(ctx context.Context, userID string, maxDepth int) (map[string]int, error)
}

// PenaltySpec describes one penalty to be applied, before it is given an
// identity or persisted. Depth 0 is the direct penalty on the offender;
// deeper entries are propagated to the people who vouched for them.
type PenaltySpec struct {
	UserID string
	Amount float64
	Depth  int
}

// planDirectPenalty returns the penalty applied to the offender themselves.
func planDirectPenalty(targetUserID string, severity int) PenaltySpec {
	return PenaltySpec{
		UserID: targetUserID,
		Amount: domain.DirectPenalty[severity],
		Depth:  0,
	}
}

// planPropagatedPenalties computes the penalties owed by the people who
// vouched for the offender, decayed geometrically by hop distance. This is the
// mechanism that gives vouchers a stake in who they endorse.
//
// The result is ordered by depth and then user ID so that the same inputs
// always produce the same plan — Go map iteration order is randomized, so
// building this list straight from the vouchers map would make both the stored
// order and any test assertion over it unstable.
//
// A voucher at depth 0, or one that is the offender themselves, would
// double-penalize the offender on top of the direct penalty, so both are
// skipped.
func planPropagatedPenalties(targetUserID string, severity int, vouchers map[string]int) []PenaltySpec {
	basePenalty := domain.DirectPenalty[severity]
	decayRate := domain.PropagationDecay[severity]

	specs := make([]PenaltySpec, 0, len(vouchers))
	for voucherID, depth := range vouchers {
		if depth <= 0 || voucherID == targetUserID {
			continue
		}
		specs = append(specs, PenaltySpec{
			UserID: voucherID,
			Amount: basePenalty * math.Pow(decayRate, float64(depth)),
			Depth:  depth,
		})
	}

	sort.Slice(specs, func(i, j int) bool {
		if specs[i].Depth != specs[j].Depth {
			return specs[i].Depth < specs[j].Depth
		}
		return specs[i].UserID < specs[j].UserID
	})

	return specs
}

// penaltyDecayTime returns when penalties from an action of this severity stop
// applying, or nil when the severity carries a permanent penalty.
func penaltyDecayTime(severity int, now time.Time) *time.Time {
	decayDays := domain.PenaltyDecayDays[severity]
	if decayDays <= 0 {
		return nil
	}
	t := now.AddDate(0, 0, decayDays)
	return &t
}

// ModerationService orchestrates trust penalty propagation.
type ModerationService struct {
	penalties  PenaltyRepository
	graph      PenaltyGraphQuerier
	now        func() time.Time
	trustQueue TrustRecalcQueue
	logger     *slog.Logger
}

func NewModerationService(penalties PenaltyRepository, graph PenaltyGraphQuerier, clock func() time.Time) *ModerationService {
	if clock == nil {
		clock = time.Now
	}
	return &ModerationService{
		penalties: penalties,
		graph:     graph,
		now:       clock,
		logger:    slog.Default(),
	}
}

// SetTrustQueue attaches an optional trust recalculation queue, mirroring
// PostService.SetFeedCache. Deployments without Redis leave it nil and simply
// do not recalculate.
func (s *ModerationService) SetTrustQueue(q TrustRecalcQueue) {
	s.trustQueue = q
}

// enqueueRecalc asks for a user's trust score to be recomputed. A penalty that
// is already committed must not be undone because the queue is unreachable, so
// this only ever logs.
func (s *ModerationService) enqueueRecalc(ctx context.Context, userID string) {
	if s.trustQueue == nil {
		return
	}
	if err := s.trustQueue.EnqueueRecalc(ctx, userID); err != nil {
		s.logger.Warn("enqueueing trust recalculation failed", "user_id", userID, "error", err)
	}
}

// PropagatePenalties creates trust penalties for a moderation action. It applies
// a direct penalty to the target user and propagated penalties to their vouchers,
// decaying by depth according to the severity configuration.
func (s *ModerationService) PropagatePenalties(ctx context.Context, actionID, targetUserID string, severity int) ([]domain.TrustPenalty, error) {
	if severity < 1 || severity > 5 {
		return nil, fmt.Errorf("%w: severity must be between 1 and 5, got %d", ErrValidation, severity)
	}

	now := s.now()
	decaysAt := penaltyDecayTime(severity, now)

	persist := func(spec PenaltySpec) (domain.TrustPenalty, error) {
		penaltyID, err := uuid.NewV7()
		if err != nil {
			return domain.TrustPenalty{}, fmt.Errorf("generating penalty id: %w", err)
		}
		penalty := domain.TrustPenalty{
			ID:                 penaltyID.String(),
			UserID:             spec.UserID,
			ModerationActionID: actionID,
			PenaltyAmount:      spec.Amount,
			HopDepth:           spec.Depth,
			CreatedAt:          now,
			DecaysAt:           decaysAt,
		}
		if err := s.penalties.CreateTrustPenalty(ctx, &penalty); err != nil {
			return domain.TrustPenalty{}, err
		}
		// The penalty has landed, so this user's moderation component has
		// changed and their score is now stale.
		s.enqueueRecalc(ctx, spec.UserID)
		return penalty, nil
	}

	// The offender's own penalty is written first and deliberately does not
	// depend on the vouch graph: if the graph is unavailable the offender must
	// still lose trust, otherwise a ban would leave their score untouched.
	direct, err := persist(planDirectPenalty(targetUserID, severity))
	if err != nil {
		return nil, fmt.Errorf("creating direct penalty: %w", err)
	}
	results := []domain.TrustPenalty{direct}

	vouchers, err := s.graph.FindVouchersWithDepth(ctx, targetUserID, domain.PropagationDepth[severity])
	if err != nil {
		return results, fmt.Errorf("querying vouch graph: %w", err)
	}

	for _, spec := range planPropagatedPenalties(targetUserID, severity, vouchers) {
		penalty, err := persist(spec)
		if err != nil {
			return results, fmt.Errorf("creating propagated penalty for %s: %w", spec.UserID, err)
		}
		results = append(results, penalty)
	}

	return results, nil
}

package service

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"math"
	"slices"
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

// penaltyPolicy is everything a moderation action's severity decides about the
// penalties it causes: what the offender loses, how much of that survives each
// hop outward, how far it travels, and how long it lasts.
//
// The planners below take one of these rather than a raw severity, and one can
// only be built for a severity the domain recognizes — so, like actionRequest,
// holding one is proof the validation passed and no planner has to re-check.
type penaltyPolicy struct {
	directPenalty float64
	decayRate     float64
	maxDepth      int
	decayDays     int
}

// resolvePenaltyPolicy looks up every severity-dependent parameter at once, so
// an unrecognized severity is rejected in a single place and before anything is
// written.
//
// This used to be four map index expressions spread across three functions,
// each of which answered an unknown severity with a zero: a penalty worth no
// points, a traversal of no hops, a decay rate that erases the penalty after
// one hop, and a decay window of 0 — which CalcModerationScore reads as
// *permanent*, the strongest outcome rather than the weakest.
func resolvePenaltyPolicy(severity int) (penaltyPolicy, error) {
	directPenalty, okPenalty := domain.DirectPenalty(severity)
	decayRate, okDecay := domain.PropagationDecay(severity)
	maxDepth, okDepth := domain.PropagationDepth(severity)
	decayDays, okDays := domain.PenaltyDecayDays(severity)

	if !okPenalty || !okDecay || !okDepth || !okDays {
		return penaltyPolicy{}, fmt.Errorf(
			"%w: severity must be between 1 and 5, got %d", ErrValidation, severity)
	}

	return penaltyPolicy{
		directPenalty: directPenalty,
		decayRate:     decayRate,
		maxDepth:      maxDepth,
		decayDays:     decayDays,
	}, nil
}

// planDirectPenalty returns the penalty applied to the offender themselves.
func planDirectPenalty(targetUserID string, policy penaltyPolicy) PenaltySpec {
	return PenaltySpec{
		UserID: targetUserID,
		Amount: policy.directPenalty,
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
func planPropagatedPenalties(targetUserID string, policy penaltyPolicy, vouchers map[string]int) []PenaltySpec {
	specs := make([]PenaltySpec, 0, len(vouchers))
	for voucherID, depth := range vouchers {
		if depth <= 0 || voucherID == targetUserID {
			continue
		}
		specs = append(specs, PenaltySpec{
			UserID: voucherID,
			Amount: policy.directPenalty * math.Pow(policy.decayRate, float64(depth)),
			Depth:  depth,
		})
	}

	slices.SortFunc(specs, func(a, b PenaltySpec) int {
		return cmp.Or(cmp.Compare(a.Depth, b.Depth), cmp.Compare(a.UserID, b.UserID))
	})

	return specs
}

// penaltyDecayTime returns when penalties from an action of this severity stop
// applying, or nil when the severity carries a permanent penalty.
//
// A decay window of 0 means permanent, which is the same number an unknown
// severity would once have produced. Only a resolved policy reaches here, so
// the 0 can now be read as permanent without ambiguity.
func penaltyDecayTime(policy penaltyPolicy, now time.Time) *time.Time {
	if policy.decayDays <= 0 {
		return nil
	}
	t := now.AddDate(0, 0, policy.decayDays)
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
	// This doubles as the severity range check. Resolving the policy up front is
	// what lets the planners below be total functions: nothing downstream can be
	// handed a severity the domain does not recognize.
	policy, err := resolvePenaltyPolicy(severity)
	if err != nil {
		return nil, err
	}

	now := s.now()
	decaysAt := penaltyDecayTime(policy, now)

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
	direct, err := persist(planDirectPenalty(targetUserID, policy))
	if err != nil {
		return nil, fmt.Errorf("creating direct penalty: %w", err)
	}
	results := []domain.TrustPenalty{direct}

	vouchers, err := s.graph.FindVouchersWithDepth(ctx, targetUserID, policy.maxDepth)
	if err != nil {
		return results, fmt.Errorf("querying vouch graph: %w", err)
	}

	for _, spec := range planPropagatedPenalties(targetUserID, policy, vouchers) {
		penalty, err := persist(spec)
		if err != nil {
			return results, fmt.Errorf("creating propagated penalty for %s: %w", spec.UserID, err)
		}
		results = append(results, penalty)
	}

	return results, nil
}

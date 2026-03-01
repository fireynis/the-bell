package service

import (
	"context"
	"fmt"
	"math"
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

// ModerationService orchestrates trust penalty propagation.
type ModerationService struct {
	penalties PenaltyRepository
	graph     PenaltyGraphQuerier
	now       func() time.Time
}

func NewModerationService(penalties PenaltyRepository, graph PenaltyGraphQuerier, clock func() time.Time) *ModerationService {
	if clock == nil {
		clock = time.Now
	}
	return &ModerationService{
		penalties: penalties,
		graph:     graph,
		now:       clock,
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
	basePenalty := domain.DirectPenalty[severity]
	decayRate := domain.PropagationDecay[severity]
	maxHops := domain.PropagationDepth[severity]
	decayDays := domain.PenaltyDecayDays[severity]

	var decaysAt *time.Time
	if decayDays > 0 {
		t := now.AddDate(0, 0, decayDays)
		decaysAt = &t
	}

	// Create direct penalty for the target user.
	directID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generating penalty id: %w", err)
	}
	direct := domain.TrustPenalty{
		ID:                 directID.String(),
		UserID:             targetUserID,
		ModerationActionID: actionID,
		PenaltyAmount:      basePenalty,
		HopDepth:           0,
		CreatedAt:          now,
		DecaysAt:           decaysAt,
	}
	if err := s.penalties.CreateTrustPenalty(ctx, &direct); err != nil {
		return nil, fmt.Errorf("creating direct penalty: %w", err)
	}

	results := []domain.TrustPenalty{direct}

	// Find vouchers and apply propagated penalties.
	vouchers, err := s.graph.FindVouchersWithDepth(ctx, targetUserID, maxHops)
	if err != nil {
		return results, fmt.Errorf("querying vouch graph: %w", err)
	}

	for voucherID, depth := range vouchers {
		amount := basePenalty * math.Pow(decayRate, float64(depth))

		penaltyID, err := uuid.NewV7()
		if err != nil {
			return results, fmt.Errorf("generating penalty id: %w", err)
		}
		penalty := domain.TrustPenalty{
			ID:                 penaltyID.String(),
			UserID:             voucherID,
			ModerationActionID: actionID,
			PenaltyAmount:      amount,
			HopDepth:           depth,
			CreatedAt:          now,
			DecaysAt:           decaysAt,
		}
		if err := s.penalties.CreateTrustPenalty(ctx, &penalty); err != nil {
			return results, fmt.Errorf("creating propagated penalty for %s: %w", voucherID, err)
		}
		results = append(results, penalty)
	}

	return results, nil
}

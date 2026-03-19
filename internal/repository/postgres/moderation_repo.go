package postgres

import (
	"context"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/jackc/pgx/v5/pgtype"
)

// ModerationActionRepo adapts sqlc queries to the service.ModerationActionRepository interface.
type ModerationActionRepo struct {
	q *Queries
}

func NewModerationActionRepo(q *Queries) *ModerationActionRepo {
	return &ModerationActionRepo{q: q}
}

func (r *ModerationActionRepo) CreateModerationAction(ctx context.Context, action *domain.ModerationAction) error {
	var durationSeconds pgtype.Int8
	if action.Duration != nil {
		durationSeconds = pgtype.Int8{Int64: int64(action.Duration.Seconds()), Valid: true}
	}

	var expiresAt pgtype.Timestamptz
	if action.ExpiresAt != nil {
		expiresAt = pgtype.Timestamptz{Time: *action.ExpiresAt, Valid: true}
	}

	_, err := r.q.CreateModerationAction(ctx, CreateModerationActionParams{
		ID:              action.ID,
		TargetUserID:    action.TargetUserID,
		ModeratorID:     action.ModeratorID,
		ActionType:      string(action.Action),
		Severity:        int32(action.Severity),
		Reason:          action.Reason,
		DurationSeconds: durationSeconds,
		CreatedAt:       pgtype.Timestamptz{Time: action.CreatedAt, Valid: true},
		ExpiresAt:       expiresAt,
	})
	return err
}

func (r *ModerationActionRepo) ListActionsByTarget(ctx context.Context, targetUserID string, limit, offset int) ([]*domain.ModerationAction, error) {
	rows, err := r.q.ListModerationActionsByTarget(ctx, ListModerationActionsByTargetParams{
		TargetUserID: targetUserID,
		Limit:        int32(limit),
		Offset:       int32(offset),
	})
	if err != nil {
		return nil, err
	}

	actions := make([]*domain.ModerationAction, len(rows))
	for i, row := range rows {
		actions[i] = moderationActionFromRow(row)
	}
	return actions, nil
}

func (r *ModerationActionRepo) ListActionsByModerator(ctx context.Context, moderatorID string, limit, offset int) ([]*domain.ModerationAction, error) {
	rows, err := r.q.ListModerationActionsByModerator(ctx, ListModerationActionsByModeratorParams{
		ModeratorID: moderatorID,
		Limit:       int32(limit),
		Offset:      int32(offset),
	})
	if err != nil {
		return nil, err
	}

	actions := make([]*domain.ModerationAction, len(rows))
	for i, row := range rows {
		actions[i] = moderationActionFromRow(row)
	}
	return actions, nil
}

func moderationActionFromRow(row ModerationAction) *domain.ModerationAction {
	a := &domain.ModerationAction{
		ID:           row.ID,
		TargetUserID: row.TargetUserID,
		ModeratorID:  row.ModeratorID,
		Action:       domain.ActionType(row.ActionType),
		Severity:     int(row.Severity),
		Reason:       row.Reason,
		CreatedAt:    row.CreatedAt.Time,
	}
	if row.DurationSeconds.Valid {
		d := time.Duration(row.DurationSeconds.Int64) * time.Second
		a.Duration = &d
	}
	if row.ExpiresAt.Valid {
		t := row.ExpiresAt.Time
		a.ExpiresAt = &t
	}
	return a
}

// PenaltyRepo adapts sqlc queries to the service.PenaltyLister interface
// (which embeds service.PenaltyRepository).
type PenaltyRepo struct {
	q *Queries
}

func NewPenaltyRepo(q *Queries) *PenaltyRepo {
	return &PenaltyRepo{q: q}
}

func (r *PenaltyRepo) CreateTrustPenalty(ctx context.Context, penalty *domain.TrustPenalty) error {
	var decaysAt pgtype.Timestamptz
	if penalty.DecaysAt != nil {
		decaysAt = pgtype.Timestamptz{Time: *penalty.DecaysAt, Valid: true}
	}

	_, err := r.q.CreateTrustPenalty(ctx, CreateTrustPenaltyParams{
		ID:                 penalty.ID,
		UserID:             penalty.UserID,
		ModerationActionID: penalty.ModerationActionID,
		PenaltyAmount:      penalty.PenaltyAmount,
		HopDepth:           int32(penalty.HopDepth),
		CreatedAt:          pgtype.Timestamptz{Time: penalty.CreatedAt, Valid: true},
		DecaysAt:           decaysAt,
	})
	return err
}

func (r *PenaltyRepo) ListActivePenaltiesByUser(ctx context.Context, userID string) ([]domain.TrustPenalty, error) {
	rows, err := r.q.ListActivePenaltiesByUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	penalties := make([]domain.TrustPenalty, len(rows))
	for i, row := range rows {
		penalties[i] = trustPenaltyFromRow(row)
	}
	return penalties, nil
}

func (r *PenaltyRepo) ListPenaltiesByActionID(ctx context.Context, actionID string) ([]domain.TrustPenalty, error) {
	rows, err := r.q.ListTrustPenaltiesByActionID(ctx, actionID)
	if err != nil {
		return nil, err
	}

	penalties := make([]domain.TrustPenalty, len(rows))
	for i, row := range rows {
		penalties[i] = trustPenaltyFromRow(row)
	}
	return penalties, nil
}

func trustPenaltyFromRow(row TrustPenalty) domain.TrustPenalty {
	p := domain.TrustPenalty{
		ID:                 row.ID,
		UserID:             row.UserID,
		ModerationActionID: row.ModerationActionID,
		PenaltyAmount:      row.PenaltyAmount,
		HopDepth:           int(row.HopDepth),
		CreatedAt:          row.CreatedAt.Time,
	}
	if row.DecaysAt.Valid {
		t := row.DecaysAt.Time
		p.DecaysAt = &t
	}
	return p
}

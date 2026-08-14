package postgres

import (
	"context"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/jackc/pgx/v5/pgtype"
)

// ModerationReliefRepo adapts sqlc queries to the moderation relief repository
// interface: the durable record of moderators lifting restrictions rather than
// imposing them.
type ModerationReliefRepo struct {
	q *Queries
}

func NewModerationReliefRepo(q *Queries) *ModerationReliefRepo {
	return &ModerationReliefRepo{q: q}
}

func (r *ModerationReliefRepo) CreateModerationRelief(ctx context.Context, relief *domain.ModerationRelief) error {
	var previous pgtype.Timestamptz
	if relief.PreviousExpiresAt != nil {
		previous = pgtype.Timestamptz{Time: *relief.PreviousExpiresAt, Valid: true}
	}

	_, err := r.q.CreateModerationRelief(ctx, CreateModerationReliefParams{
		ID:                relief.ID,
		TargetUserID:      relief.TargetUserID,
		ModeratorID:       relief.ModeratorID,
		ReliefType:        string(relief.Type),
		PreviousExpiresAt: previous,
		WasInForce:        relief.WasInForce,
		CreatedAt:         pgtype.Timestamptz{Time: relief.CreatedAt, Valid: true},
	})
	return err
}

// ListLiftsInForce returns the lifts of the given type that released this user
// from a live restriction, newest first.
//
// The relief type is a parameter so that mute lifts and suspension lifts share
// one read: they are the same row shape distinguished by relief_type, and a
// second copy of this method would be a second place for the filter below to be
// got wrong.
//
// The was_in_force filter is in SQL, not applied to the result here: a lift
// against somebody who was not restricted is still a recorded event, and
// filtering after LIMIT would let a run of those hide the release that actually
// freed them.
func (r *ModerationReliefRepo) ListLiftsInForce(ctx context.Context, targetUserID string, reliefType domain.ReliefType, limit int) ([]domain.ModerationRelief, error) {
	rows, err := r.q.ListLiftsInForceByTarget(ctx, ListLiftsInForceByTargetParams{
		TargetUserID: targetUserID,
		ReliefType:   string(reliefType),
		Limit:        int32(limit),
	})
	if err != nil {
		return nil, err
	}

	reliefs := make([]domain.ModerationRelief, len(rows))
	for i, row := range rows {
		reliefs[i] = moderationReliefFromRow(row)
	}
	return reliefs, nil
}

func moderationReliefFromRow(row ModerationRelief) domain.ModerationRelief {
	r := domain.ModerationRelief{
		ID:           row.ID,
		TargetUserID: row.TargetUserID,
		ModeratorID:  row.ModeratorID,
		Type:         domain.ReliefType(row.ReliefType),
		WasInForce:   row.WasInForce,
		CreatedAt:    row.CreatedAt.Time,
	}
	if row.PreviousExpiresAt.Valid {
		t := row.PreviousExpiresAt.Time
		r.PreviousExpiresAt = &t
	}
	return r
}

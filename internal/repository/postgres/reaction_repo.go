package postgres

import (
	"context"
	"errors"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ReactionRepo adapts sqlc queries to the service.ReactionRepository interface.
type ReactionRepo struct {
	q *Queries
}

func NewReactionRepo(q *Queries) *ReactionRepo {
	return &ReactionRepo{q: q}
}

func (r *ReactionRepo) AddReaction(ctx context.Context, reaction *domain.Reaction) error {
	_, err := r.q.AddReaction(ctx, AddReactionParams{
		ID:           reaction.ID,
		UserID:       reaction.UserID,
		PostID:       reaction.PostID,
		ReactionType: string(reaction.Type),
		CreatedAt:    pgtype.Timestamptz{Time: reaction.CreatedAt, Valid: true},
	})
	return err
}

func (r *ReactionRepo) RemoveReaction(ctx context.Context, userID, postID string, reactionType domain.ReactionType) error {
	return r.q.RemoveReaction(ctx, RemoveReactionParams{
		UserID:       userID,
		PostID:       postID,
		ReactionType: string(reactionType),
	})
}

func (r *ReactionRepo) CountByPost(ctx context.Context, postID string) (map[domain.ReactionType]int, error) {
	rows, err := r.q.CountReactionsByPost(ctx, postID)
	if err != nil {
		return nil, err
	}

	counts := make(map[domain.ReactionType]int, len(rows))
	for _, row := range rows {
		counts[domain.ReactionType(row.ReactionType)] = int(row.Count)
	}
	return counts, nil
}

func (r *ReactionRepo) GetUserReaction(ctx context.Context, userID, postID string, reactionType domain.ReactionType) (*domain.Reaction, error) {
	row, err := r.q.GetUserReactionOnPost(ctx, GetUserReactionOnPostParams{
		UserID:       userID,
		PostID:       postID,
		ReactionType: string(reactionType),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return reactionFromRow(row), nil
}

func (r *ReactionRepo) ListByPost(ctx context.Context, postID string) ([]*domain.Reaction, error) {
	rows, err := r.q.ListReactionsByPost(ctx, postID)
	if err != nil {
		return nil, err
	}

	reactions := make([]*domain.Reaction, len(rows))
	for i, row := range rows {
		reactions[i] = reactionFromRow(row)
	}
	return reactions, nil
}

func reactionFromRow(row Reaction) *domain.Reaction {
	return &domain.Reaction{
		ID:        row.ID,
		UserID:    row.UserID,
		PostID:    row.PostID,
		Type:      domain.ReactionType(row.ReactionType),
		CreatedAt: row.CreatedAt.Time,
	}
}

package postgres

import (
	"context"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/jackc/pgx/v5/pgtype"
)

// ReactionRepo adapts sqlc queries to the service.ReactionRepository interface,
// plus the batch reads behind handler.ReactionEnricher.
//
// There are deliberately no single-post reads here. The feed loads reactions
// for a whole page in two batch queries, so a per-post count or lookup would
// only ever be called by a test.
type ReactionRepo struct {
	q *Queries
}

func NewReactionRepo(q *Queries) *ReactionRepo {
	return &ReactionRepo{q: q}
}

// AddReaction is idempotent: reacting twice is ordinary user behaviour — a
// double-tap, a retried request — so queries/reactions.sql upserts rather than
// raising a unique violation. A repeat returns nil and leaves the original
// created_at intact. Callers must not expect a duplicate to be reported.
//
// The upsert does not cover the post_id foreign key, though. ON CONFLICT
// resolves an index conflict; a foreign key is a referential trigger that fires
// anyway. So reacting to a post that does not exist — a stale feed card, a
// post removed between render and tap — raises 23503, which is reported as
// domain.ErrNotFound so the caller gets a 404 rather than a 500.
func (r *ReactionRepo) AddReaction(ctx context.Context, reaction *domain.Reaction) error {
	_, err := r.q.AddReaction(ctx, AddReactionParams{
		ID:           reaction.ID,
		UserID:       reaction.UserID,
		PostID:       reaction.PostID,
		ReactionType: string(reaction.Type),
		CreatedAt:    pgtype.Timestamptz{Time: reaction.CreatedAt, Valid: true},
	})
	if isForeignKeyViolation(err) {
		return domain.ErrNotFound
	}
	return err
}

func (r *ReactionRepo) RemoveReaction(ctx context.Context, userID, postID string, reactionType domain.ReactionType) error {
	return r.q.RemoveReaction(ctx, RemoveReactionParams{
		UserID:       userID,
		PostID:       postID,
		ReactionType: string(reactionType),
	})
}

func (r *ReactionRepo) CountReactionsReceivedByAuthorSince(ctx context.Context, authorID string, since time.Time) (int64, error) {
	return r.q.CountReactionsReceivedByAuthorSince(ctx, CountReactionsReceivedByAuthorSinceParams{
		AuthorID:  authorID,
		CreatedAt: pgtype.Timestamptz{Time: since, Valid: true},
	})
}

func (r *ReactionRepo) BatchCountByPosts(ctx context.Context, postIDs []string) (map[string]map[domain.ReactionType]int, error) {
	rows, err := r.q.BatchCountReactionsByPosts(ctx, postIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]map[domain.ReactionType]int)
	for _, row := range rows {
		if result[row.PostID] == nil {
			result[row.PostID] = make(map[domain.ReactionType]int)
		}
		result[row.PostID][domain.ReactionType(row.ReactionType)] = int(row.Count)
	}
	return result, nil
}

func (r *ReactionRepo) BatchGetUserReactions(ctx context.Context, userID string, postIDs []string) (map[string][]domain.ReactionType, error) {
	rows, err := r.q.BatchGetUserReactionsForPosts(ctx, BatchGetUserReactionsForPostsParams{
		UserID:  userID,
		PostIds: postIDs,
	})
	if err != nil {
		return nil, err
	}
	result := make(map[string][]domain.ReactionType)
	for _, row := range rows {
		result[row.PostID] = append(result[row.PostID], domain.ReactionType(row.ReactionType))
	}
	return result, nil
}

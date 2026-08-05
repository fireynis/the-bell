package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/google/uuid"
)

var ErrInvalidReactionType = errors.New("invalid reaction type")

// ReactionRepository abstracts reaction persistence using domain types.
//
// Reads are absent by design: the feed loads reactions for a page of posts in
// one go through handler.ReactionEnricher, so this service only ever writes.
type ReactionRepository interface {
	AddReaction(ctx context.Context, reaction *domain.Reaction) error
	RemoveReaction(ctx context.Context, userID, postID string, reactionType domain.ReactionType) error
}

// ReactionService orchestrates reaction business logic.
type ReactionService struct {
	repo ReactionRepository
	now  func() time.Time
}

func NewReactionService(repo ReactionRepository, clock func() time.Time) *ReactionService {
	if clock == nil {
		clock = time.Now
	}
	return &ReactionService{repo: repo, now: clock}
}

var validReactionTypes = map[domain.ReactionType]bool{
	domain.ReactionBell:      true,
	domain.ReactionHeart:     true,
	domain.ReactionCelebrate: true,
}

func (s *ReactionService) Add(ctx context.Context, userID, postID string, reactionType domain.ReactionType) (*domain.Reaction, error) {
	if !validReactionTypes[reactionType] {
		return nil, fmt.Errorf("%w: %s", ErrInvalidReactionType, reactionType)
	}
	reaction := &domain.Reaction{
		ID:        uuid.Must(uuid.NewV7()).String(),
		UserID:    userID,
		PostID:    postID,
		Type:      reactionType,
		CreatedAt: s.now(),
	}
	if err := s.repo.AddReaction(ctx, reaction); err != nil {
		return nil, fmt.Errorf("adding reaction: %w", err)
	}
	return reaction, nil
}

// Remove withdraws one of the user's reactions from a post.
//
// It is deliberately idempotent: removing a reaction that is not there succeeds
// and reports nothing. A reaction is a toggle, so a double-tap or a retried
// request is ordinary use rather than an error, and the client that sent it
// already has the state it asked for.
//
// The repository does not report a row count and must not start doing so.
// queries/reactions.sql RemoveReaction is a plain :exec DELETE, which matches no
// rows and returns nil — pinned by
// TestReactionRepo_RemoveReaction_NotPresentIsNotAnError in
// internal/repository/postgres. Adding a rows-affected check here to return a
// "not found" error would turn a harmless no-op into a 404, which the frontend
// reaction button treats as a failed toggle and visually reverts (see
// web/src/components/ReactionButton.tsx) — showing the reaction as still present
// when the server has already removed it.
func (s *ReactionService) Remove(ctx context.Context, userID, postID string, reactionType domain.ReactionType) error {
	if !validReactionTypes[reactionType] {
		return fmt.Errorf("%w: %s", ErrInvalidReactionType, reactionType)
	}
	return s.repo.RemoveReaction(ctx, userID, postID, reactionType)
}

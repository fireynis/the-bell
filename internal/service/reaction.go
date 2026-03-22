package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/google/uuid"
)

var (
	ErrInvalidReactionType = errors.New("invalid reaction type")
	ErrReactionNotFound    = errors.New("reaction not found")
)

// ReactionRepository abstracts reaction persistence using domain types.
type ReactionRepository interface {
	AddReaction(ctx context.Context, reaction *domain.Reaction) error
	RemoveReaction(ctx context.Context, userID, postID string, reactionType domain.ReactionType) error
	CountByPost(ctx context.Context, postID string) (map[domain.ReactionType]int, error)
	GetUserReaction(ctx context.Context, userID, postID string, reactionType domain.ReactionType) (*domain.Reaction, error)
	ListByPost(ctx context.Context, postID string) ([]*domain.Reaction, error)
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

func (s *ReactionService) Remove(ctx context.Context, userID, postID string, reactionType domain.ReactionType) error {
	if !validReactionTypes[reactionType] {
		return fmt.Errorf("%w: %s", ErrInvalidReactionType, reactionType)
	}
	return s.repo.RemoveReaction(ctx, userID, postID, reactionType)
}

func (s *ReactionService) CountByPost(ctx context.Context, postID string) (map[domain.ReactionType]int, error) {
	return s.repo.CountByPost(ctx, postID)
}

func (s *ReactionService) ListByPost(ctx context.Context, postID string) ([]*domain.Reaction, error) {
	return s.repo.ListByPost(ctx, postID)
}

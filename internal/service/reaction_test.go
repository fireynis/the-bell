package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// mockReactionRepo is an in-memory ReactionRepository for testing.
type mockReactionRepo struct {
	reactions map[string]*domain.Reaction // keyed by "userID:postID:type"
}

func newMockReactionRepo() *mockReactionRepo {
	return &mockReactionRepo{reactions: make(map[string]*domain.Reaction)}
}

func reactionKey(userID, postID string, rt domain.ReactionType) string {
	return fmt.Sprintf("%s:%s:%s", userID, postID, rt)
}

func (m *mockReactionRepo) AddReaction(_ context.Context, reaction *domain.Reaction) error {
	key := reactionKey(reaction.UserID, reaction.PostID, reaction.Type)
	m.reactions[key] = reaction
	return nil
}

// Mirrors the real adapter: queries/reactions.sql RemoveReaction is a plain
// :exec DELETE, so removing something that is not there matches no rows and
// returns nil. A fake that invented a "not found" error here is what let an
// unreachable 404 sit in the handler untested for as long as it did.
func (m *mockReactionRepo) RemoveReaction(_ context.Context, userID, postID string, reactionType domain.ReactionType) error {
	delete(m.reactions, reactionKey(userID, postID, reactionType))
	return nil
}

func TestReactionService_Add(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	repo := newMockReactionRepo()
	svc := NewReactionService(repo, clock)

	reaction, err := svc.Add(context.Background(), "user-1", "post-1", domain.ReactionBell)
	if err != nil {
		t.Fatalf("Add() unexpected error: %v", err)
	}
	if reaction.ID == "" {
		t.Error("Add() returned empty ID")
	}
	if reaction.UserID != "user-1" {
		t.Errorf("UserID = %q, want %q", reaction.UserID, "user-1")
	}
	if reaction.PostID != "post-1" {
		t.Errorf("PostID = %q, want %q", reaction.PostID, "post-1")
	}
	if reaction.Type != domain.ReactionBell {
		t.Errorf("Type = %q, want %q", reaction.Type, domain.ReactionBell)
	}
	if !reaction.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", reaction.CreatedAt, now)
	}

	// Verify reaction was stored in repo
	key := reactionKey("user-1", "post-1", domain.ReactionBell)
	if _, ok := repo.reactions[key]; !ok {
		t.Error("reaction not stored in repository")
	}
}

func TestReactionService_Add_InvalidType(t *testing.T) {
	repo := newMockReactionRepo()
	svc := NewReactionService(repo, nil)

	_, err := svc.Add(context.Background(), "user-1", "post-1", domain.ReactionType("invalid"))
	if err == nil {
		t.Fatal("Add() expected error for invalid type, got nil")
	}
	if !errors.Is(err, ErrInvalidReactionType) {
		t.Fatalf("Add() error = %v, want %v", err, ErrInvalidReactionType)
	}

	// Verify nothing was stored
	if len(repo.reactions) != 0 {
		t.Errorf("expected empty repo, got %d reactions", len(repo.reactions))
	}
}

func TestReactionService_Remove(t *testing.T) {
	repo := newMockReactionRepo()
	svc := NewReactionService(repo, nil)

	// Seed a reaction
	repo.reactions[reactionKey("user-1", "post-1", domain.ReactionHeart)] = &domain.Reaction{
		ID:     "r-1",
		UserID: "user-1",
		PostID: "post-1",
		Type:   domain.ReactionHeart,
	}

	err := svc.Remove(context.Background(), "user-1", "post-1", domain.ReactionHeart)
	if err != nil {
		t.Fatalf("Remove() unexpected error: %v", err)
	}

	// Verify reaction was removed
	key := reactionKey("user-1", "post-1", domain.ReactionHeart)
	if _, ok := repo.reactions[key]; ok {
		t.Error("reaction still present in repository after Remove()")
	}
}

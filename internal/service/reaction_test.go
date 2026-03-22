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

func (m *mockReactionRepo) RemoveReaction(_ context.Context, userID, postID string, reactionType domain.ReactionType) error {
	key := reactionKey(userID, postID, reactionType)
	if _, ok := m.reactions[key]; !ok {
		return ErrReactionNotFound
	}
	delete(m.reactions, key)
	return nil
}

func (m *mockReactionRepo) CountByPost(_ context.Context, postID string) (map[domain.ReactionType]int, error) {
	counts := make(map[domain.ReactionType]int)
	for _, r := range m.reactions {
		if r.PostID == postID {
			counts[r.Type]++
		}
	}
	return counts, nil
}

func (m *mockReactionRepo) GetUserReaction(_ context.Context, userID, postID string, reactionType domain.ReactionType) (*domain.Reaction, error) {
	key := reactionKey(userID, postID, reactionType)
	r, ok := m.reactions[key]
	if !ok {
		return nil, ErrReactionNotFound
	}
	return r, nil
}

func (m *mockReactionRepo) ListByPost(_ context.Context, postID string) ([]*domain.Reaction, error) {
	var result []*domain.Reaction
	for _, r := range m.reactions {
		if r.PostID == postID {
			result = append(result, r)
		}
	}
	return result, nil
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

func TestReactionService_CountByPost(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	repo := newMockReactionRepo()
	svc := NewReactionService(repo, clock)

	// Add multiple reactions to the same post
	if _, err := svc.Add(context.Background(), "user-1", "post-1", domain.ReactionBell); err != nil {
		t.Fatalf("Add() unexpected error: %v", err)
	}
	if _, err := svc.Add(context.Background(), "user-2", "post-1", domain.ReactionBell); err != nil {
		t.Fatalf("Add() unexpected error: %v", err)
	}
	if _, err := svc.Add(context.Background(), "user-3", "post-1", domain.ReactionHeart); err != nil {
		t.Fatalf("Add() unexpected error: %v", err)
	}

	// Add a reaction to a different post (should not appear in counts)
	if _, err := svc.Add(context.Background(), "user-1", "post-2", domain.ReactionCelebrate); err != nil {
		t.Fatalf("Add() unexpected error: %v", err)
	}

	counts, err := svc.CountByPost(context.Background(), "post-1")
	if err != nil {
		t.Fatalf("CountByPost() unexpected error: %v", err)
	}

	if counts[domain.ReactionBell] != 2 {
		t.Errorf("bell count = %d, want 2", counts[domain.ReactionBell])
	}
	if counts[domain.ReactionHeart] != 1 {
		t.Errorf("heart count = %d, want 1", counts[domain.ReactionHeart])
	}
	if counts[domain.ReactionCelebrate] != 0 {
		t.Errorf("celebrate count = %d, want 0", counts[domain.ReactionCelebrate])
	}
}

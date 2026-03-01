package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// mockPostRepo is an in-memory PostRepository for testing.
type mockPostRepo struct {
	posts map[string]*domain.Post
}

func newMockPostRepo() *mockPostRepo {
	return &mockPostRepo{posts: make(map[string]*domain.Post)}
}

func (m *mockPostRepo) CreatePost(_ context.Context, post *domain.Post) error {
	m.posts[post.ID] = post
	return nil
}

func (m *mockPostRepo) GetPostByID(_ context.Context, id string) (*domain.Post, error) {
	p, ok := m.posts[id]
	if !ok {
		return nil, ErrNotFound
	}
	return p, nil
}

func (m *mockPostRepo) ListPosts(_ context.Context, cursor string, limit int) ([]*domain.Post, error) {
	result := []*domain.Post{}
	for _, p := range m.posts {
		if p.Status != domain.PostVisible {
			continue
		}
		if cursor != "" && p.ID >= cursor {
			continue
		}
		result = append(result, p)
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *mockPostRepo) UpdatePostBody(_ context.Context, id string, body string) (*domain.Post, error) {
	p, ok := m.posts[id]
	if !ok {
		return nil, ErrNotFound
	}
	p.Body = body
	now := time.Now()
	p.EditedAt = &now
	return p, nil
}

func (m *mockPostRepo) UpdatePostStatus(_ context.Context, id string, status domain.PostStatus, reason string) error {
	p, ok := m.posts[id]
	if !ok {
		return ErrNotFound
	}
	p.Status = status
	p.RemovalReason = reason
	return nil
}

func TestPostService_Create(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	tests := []struct {
		name      string
		authorID  string
		body      string
		imagePath string
		wantErr   error
	}{
		{
			name:      "valid post",
			authorID:  "user-1",
			body:      "Hello, world!",
			imagePath: "/images/photo.jpg",
		},
		{
			name:      "valid post without image",
			authorID:  "user-1",
			body:      "Just text",
			imagePath: "",
		},
		{
			name:      "body at max length",
			authorID:  "user-1",
			body:      strings.Repeat("a", domain.MaxPostBodyLength),
			imagePath: "",
		},
		{
			name:     "empty body",
			authorID: "user-1",
			body:     "",
			wantErr:  ErrValidation,
		},
		{
			name:     "whitespace-only body",
			authorID: "user-1",
			body:     "   \t\n  ",
			wantErr:  ErrValidation,
		},
		{
			name:     "body exceeds max length",
			authorID: "user-1",
			body:     strings.Repeat("a", domain.MaxPostBodyLength+1),
			wantErr:  ErrValidation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockPostRepo()
			svc := NewPostService(repo, WithClock(clock))

			post, err := svc.Create(context.Background(), tt.authorID, tt.body, tt.imagePath)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Create() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Create() unexpected error: %v", err)
			}
			if post.ID == "" {
				t.Error("Create() returned empty ID")
			}
			if post.AuthorID != tt.authorID {
				t.Errorf("AuthorID = %q, want %q", post.AuthorID, tt.authorID)
			}
			if post.Body != tt.body {
				t.Errorf("Body = %q, want %q", post.Body, tt.body)
			}
			if post.ImagePath != tt.imagePath {
				t.Errorf("ImagePath = %q, want %q", post.ImagePath, tt.imagePath)
			}
			if post.Status != domain.PostVisible {
				t.Errorf("Status = %q, want %q", post.Status, domain.PostVisible)
			}
			if !post.CreatedAt.Equal(now) {
				t.Errorf("CreatedAt = %v, want %v", post.CreatedAt, now)
			}
			// Verify post was stored in repo
			if _, ok := repo.posts[post.ID]; !ok {
				t.Error("post not stored in repository")
			}
		})
	}
}

func TestPostService_GetByID(t *testing.T) {
	repo := newMockPostRepo()
	svc := NewPostService(repo)

	existing := &domain.Post{
		ID:       "post-1",
		AuthorID: "user-1",
		Body:     "test post",
		Status:   domain.PostVisible,
	}
	repo.posts["post-1"] = existing

	tests := []struct {
		name    string
		id      string
		wantErr error
	}{
		{"existing post", "post-1", nil},
		{"not found", "post-999", ErrNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			post, err := svc.GetByID(context.Background(), tt.id)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("GetByID() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetByID() unexpected error: %v", err)
			}
			if post.ID != tt.id {
				t.Errorf("ID = %q, want %q", post.ID, tt.id)
			}
		})
	}
}

func TestPostService_ListFeed(t *testing.T) {
	repo := newMockPostRepo()
	svc := NewPostService(repo)

	// Add visible and non-visible posts
	repo.posts["a"] = &domain.Post{ID: "a", Status: domain.PostVisible}
	repo.posts["b"] = &domain.Post{ID: "b", Status: domain.PostRemovedByAuthor}
	repo.posts["c"] = &domain.Post{ID: "c", Status: domain.PostVisible}

	posts, err := svc.ListFeed(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("ListFeed() unexpected error: %v", err)
	}

	// Only visible posts should be returned
	for _, p := range posts {
		if p.Status != domain.PostVisible {
			t.Errorf("ListFeed() returned non-visible post %q with status %q", p.ID, p.Status)
		}
	}
	if len(posts) != 2 {
		t.Errorf("ListFeed() returned %d posts, want 2", len(posts))
	}
}

func TestPostService_ListFeed_Empty(t *testing.T) {
	repo := newMockPostRepo()
	svc := NewPostService(repo)

	posts, err := svc.ListFeed(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("ListFeed() unexpected error: %v", err)
	}
	if posts == nil {
		t.Error("ListFeed() returned nil, want empty slice")
	}
}

func TestPostService_UpdateBody(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		post    *domain.Post
		userID  string
		body    string
		clock   time.Time
		wantErr error
	}{
		{
			name: "author within window",
			post: &domain.Post{
				ID:        "post-1",
				AuthorID:  "user-1",
				Body:      "original",
				Status:    domain.PostVisible,
				CreatedAt: now.Add(-10 * time.Minute),
			},
			userID: "user-1",
			body:   "updated body",
			clock:  now,
		},
		{
			name: "at exact 15-min boundary",
			post: &domain.Post{
				ID:        "post-2",
				AuthorID:  "user-1",
				Body:      "original",
				Status:    domain.PostVisible,
				CreatedAt: now.Add(-15 * time.Minute),
			},
			userID: "user-1",
			body:   "updated at boundary",
			clock:  now,
		},
		{
			name: "wrong user",
			post: &domain.Post{
				ID:        "post-3",
				AuthorID:  "user-1",
				Body:      "original",
				Status:    domain.PostVisible,
				CreatedAt: now.Add(-5 * time.Minute),
			},
			userID:  "user-2",
			body:    "hijack attempt",
			clock:   now,
			wantErr: ErrEditWindow,
		},
		{
			name: "past 15-min window",
			post: &domain.Post{
				ID:        "post-4",
				AuthorID:  "user-1",
				Body:      "original",
				Status:    domain.PostVisible,
				CreatedAt: now.Add(-20 * time.Minute),
			},
			userID:  "user-1",
			body:    "too late",
			clock:   now,
			wantErr: ErrEditWindow,
		},
		{
			name: "removed post",
			post: &domain.Post{
				ID:        "post-5",
				AuthorID:  "user-1",
				Body:      "original",
				Status:    domain.PostRemovedByAuthor,
				CreatedAt: now.Add(-5 * time.Minute),
			},
			userID:  "user-1",
			body:    "revive attempt",
			clock:   now,
			wantErr: ErrEditWindow,
		},
		{
			name:    "not found",
			post:    nil, // no post seeded
			userID:  "user-1",
			body:    "edit ghost",
			clock:   now,
			wantErr: ErrNotFound,
		},
		{
			name: "empty new body",
			post: &domain.Post{
				ID:        "post-6",
				AuthorID:  "user-1",
				Body:      "original",
				Status:    domain.PostVisible,
				CreatedAt: now.Add(-5 * time.Minute),
			},
			userID:  "user-1",
			body:    "",
			clock:   now,
			wantErr: ErrValidation,
		},
		{
			name: "body exceeds max",
			post: &domain.Post{
				ID:        "post-7",
				AuthorID:  "user-1",
				Body:      "original",
				Status:    domain.PostVisible,
				CreatedAt: now.Add(-5 * time.Minute),
			},
			userID:  "user-1",
			body:    strings.Repeat("x", domain.MaxPostBodyLength+1),
			clock:   now,
			wantErr: ErrValidation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockPostRepo()
			svc := NewPostService(repo, WithClock(func() time.Time { return tt.clock }))

			postID := "nonexistent"
			if tt.post != nil {
				repo.posts[tt.post.ID] = tt.post
				postID = tt.post.ID
			}

			updated, err := svc.UpdateBody(context.Background(), postID, tt.userID, tt.body)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("UpdateBody() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("UpdateBody() unexpected error: %v", err)
			}
			if updated.Body != tt.body {
				t.Errorf("Body = %q, want %q", updated.Body, tt.body)
			}
		})
	}
}

func TestPostService_Delete(t *testing.T) {
	tests := []struct {
		name    string
		post    *domain.Post
		userID  string
		wantErr error
	}{
		{
			name: "author deletes own post",
			post: &domain.Post{
				ID:       "post-1",
				AuthorID: "user-1",
				Body:     "to be deleted",
				Status:   domain.PostVisible,
			},
			userID: "user-1",
		},
		{
			name: "non-author forbidden",
			post: &domain.Post{
				ID:       "post-2",
				AuthorID: "user-1",
				Body:     "not yours",
				Status:   domain.PostVisible,
			},
			userID:  "user-2",
			wantErr: ErrForbidden,
		},
		{
			name:    "not found",
			post:    nil,
			userID:  "user-1",
			wantErr: ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockPostRepo()
			svc := NewPostService(repo)

			postID := "nonexistent"
			if tt.post != nil {
				repo.posts[tt.post.ID] = tt.post
				postID = tt.post.ID
			}

			err := svc.Delete(context.Background(), postID, tt.userID)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Delete() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Delete() unexpected error: %v", err)
			}

			// Verify post was soft-deleted
			p := repo.posts[postID]
			if p.Status != domain.PostRemovedByAuthor {
				t.Errorf("Status = %q, want %q", p.Status, domain.PostRemovedByAuthor)
			}
		})
	}
}

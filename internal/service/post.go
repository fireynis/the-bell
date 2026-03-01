package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/google/uuid"
)

// PostRepository abstracts post persistence using domain types.
type PostRepository interface {
	CreatePost(ctx context.Context, post *domain.Post) error
	GetPostByID(ctx context.Context, id string) (*domain.Post, error)
	ListPosts(ctx context.Context, cursor string, limit int) ([]*domain.Post, error)
	UpdatePostBody(ctx context.Context, id string, body string) (*domain.Post, error)
	UpdatePostStatus(ctx context.Context, id string, status domain.PostStatus, reason string) error
}

// PostService orchestrates post business logic.
type PostService struct {
	repo PostRepository
	now  func() time.Time
}

type PostServiceOption func(*PostService)

// WithClock overrides the clock used by PostService. Useful for testing.
func WithClock(fn func() time.Time) PostServiceOption {
	return func(s *PostService) {
		s.now = fn
	}
}

func NewPostService(repo PostRepository, opts ...PostServiceOption) *PostService {
	s := &PostService{
		repo: repo,
		now:  time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *PostService) Create(ctx context.Context, authorID, body, imagePath string) (*domain.Post, error) {
	if err := validateBody(body); err != nil {
		return nil, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generating post id: %w", err)
	}

	post := &domain.Post{
		ID:        id.String(),
		AuthorID:  authorID,
		Body:      body,
		ImagePath: imagePath,
		Status:    domain.PostVisible,
		CreatedAt: s.now(),
	}

	if err := s.repo.CreatePost(ctx, post); err != nil {
		return nil, fmt.Errorf("creating post: %w", err)
	}

	return post, nil
}

func (s *PostService) GetByID(ctx context.Context, id string) (*domain.Post, error) {
	return s.repo.GetPostByID(ctx, id)
}

func (s *PostService) ListFeed(ctx context.Context, cursor string, limit int) ([]*domain.Post, error) {
	return s.repo.ListPosts(ctx, cursor, limit)
}

func (s *PostService) UpdateBody(ctx context.Context, id, userID, body string) (*domain.Post, error) {
	if err := validateBody(body); err != nil {
		return nil, err
	}

	post, err := s.repo.GetPostByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if !post.CanEdit(userID, s.now()) {
		return nil, ErrEditWindow
	}

	return s.repo.UpdatePostBody(ctx, id, body)
}

func (s *PostService) Delete(ctx context.Context, id, userID string) error {
	post, err := s.repo.GetPostByID(ctx, id)
	if err != nil {
		return err
	}

	if post.AuthorID != userID {
		return ErrForbidden
	}

	return s.repo.UpdatePostStatus(ctx, id, domain.PostRemovedByAuthor, "")
}

func validateBody(body string) error {
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("%w: body must not be empty", ErrValidation)
	}
	if len(body) > domain.MaxPostBodyLength {
		return fmt.Errorf("%w: body exceeds %d characters", ErrValidation, domain.MaxPostBodyLength)
	}
	return nil
}

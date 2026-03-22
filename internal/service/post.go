package service

import (
	"context"
	"encoding/json"
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
	ListPostsByAuthor(ctx context.Context, authorID string, limit int) ([]*domain.Post, error)
	UpdatePostBody(ctx context.Context, id string, body string) (*domain.Post, error)
	UpdatePostStatus(ctx context.Context, id string, status domain.PostStatus, reason string) error
}

// FeedCacher is an optional cache layer for the post feed.
type FeedCacher interface {
	GetFeed(ctx context.Context, cursor string, limit int) ([]*domain.Post, error)
	InvalidateOnCreate(ctx context.Context, post *domain.Post)
	InvalidateOnDelete(ctx context.Context, postID string)
}

// EventPublisher publishes post events for real-time SSE delivery.
type EventPublisher interface {
	PublishPost(ctx context.Context, postJSON []byte) error
}

// PostService orchestrates post business logic.
type PostService struct {
	repo      PostRepository
	feedCache FeedCacher
	publisher EventPublisher
	now       func() time.Time
}

func NewPostService(repo PostRepository, clock func() time.Time) *PostService {
	if clock == nil {
		clock = time.Now
	}
	return &PostService{
		repo: repo,
		now:  clock,
	}
}

// SetFeedCache attaches an optional feed cache to the service.
func (s *PostService) SetFeedCache(fc FeedCacher) {
	s.feedCache = fc
}

// SetPublisher attaches an optional event publisher for real-time SSE delivery.
func (s *PostService) SetPublisher(pub EventPublisher) {
	s.publisher = pub
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

	if s.feedCache != nil {
		s.feedCache.InvalidateOnCreate(ctx, post)
	}

	if s.publisher != nil {
		if data, err := json.Marshal(post); err == nil {
			_ = s.publisher.PublishPost(ctx, data)
		}
	}

	return post, nil
}

func (s *PostService) GetByID(ctx context.Context, id string) (*domain.Post, error) {
	return s.repo.GetPostByID(ctx, id)
}

func (s *PostService) ListFeed(ctx context.Context, cursor string, limit int) ([]*domain.Post, error) {
	if s.feedCache != nil {
		return s.feedCache.GetFeed(ctx, cursor, limit)
	}
	return s.repo.ListPosts(ctx, cursor, limit)
}

func (s *PostService) ListByAuthor(ctx context.Context, authorID string, limit int) ([]*domain.Post, error) {
	return s.repo.ListPostsByAuthor(ctx, authorID, limit)
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

	if err := s.repo.UpdatePostStatus(ctx, id, domain.PostRemovedByAuthor, ""); err != nil {
		return err
	}

	if s.feedCache != nil {
		s.feedCache.InvalidateOnDelete(ctx, id)
	}

	return nil
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

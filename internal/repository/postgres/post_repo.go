package postgres

import (
	"context"
	"errors"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// PostRepo adapts sqlc queries to the service.PostRepository interface.
type PostRepo struct {
	q *Queries
}

func NewPostRepo(q *Queries) *PostRepo {
	return &PostRepo{q: q}
}

func (r *PostRepo) CreatePost(ctx context.Context, post *domain.Post) error {
	_, err := r.q.CreatePost(ctx, CreatePostParams{
		ID:            post.ID,
		AuthorID:      post.AuthorID,
		Body:          post.Body,
		ImagePath:     post.ImagePath,
		Status:        string(post.Status),
		RemovalReason: post.RemovalReason,
		CreatedAt:     pgtype.Timestamptz{Time: post.CreatedAt, Valid: true},
	})
	return err
}

func (r *PostRepo) GetPostByID(ctx context.Context, id string) (*domain.Post, error) {
	row, err := r.q.GetPostByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, service.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return postFromRow(row), nil
}

func (r *PostRepo) ListPosts(ctx context.Context, cursor string, limit int) ([]*domain.Post, error) {
	var rows []Post
	var err error

	if cursor == "" {
		rows, err = r.q.ListPostsFeedFirst(ctx, int32(limit))
	} else {
		rows, err = r.q.ListPostsFeed(ctx, ListPostsFeedParams{
			ID:    cursor,
			Limit: int32(limit),
		})
	}
	if err != nil {
		return nil, err
	}

	posts := make([]*domain.Post, len(rows))
	for i, row := range rows {
		posts[i] = postFromRow(row)
	}
	return posts, nil
}

func (r *PostRepo) ListPostsByAuthor(ctx context.Context, authorID string, limit int) ([]*domain.Post, error) {
	rows, err := r.q.ListPostsByAuthor(ctx, ListPostsByAuthorParams{
		AuthorID: authorID,
		Limit:    int32(limit),
	})
	if err != nil {
		return nil, err
	}

	posts := make([]*domain.Post, len(rows))
	for i, row := range rows {
		posts[i] = postFromRow(row)
	}
	return posts, nil
}

func (r *PostRepo) UpdatePostBody(ctx context.Context, id string, body string) (*domain.Post, error) {
	row, err := r.q.UpdatePostBody(ctx, UpdatePostBodyParams{
		ID:   id,
		Body: body,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, service.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return postFromRow(row), nil
}

func (r *PostRepo) UpdatePostStatus(ctx context.Context, id string, status domain.PostStatus, reason string) error {
	return r.q.SoftDeletePost(ctx, SoftDeletePostParams{
		ID:            id,
		Status:        string(status),
		RemovalReason: reason,
	})
}

func postFromRow(row Post) *domain.Post {
	p := &domain.Post{
		ID:            row.ID,
		AuthorID:      row.AuthorID,
		Body:          row.Body,
		ImagePath:     row.ImagePath,
		Status:        domain.PostStatus(row.Status),
		RemovalReason: row.RemovalReason,
		CreatedAt:     row.CreatedAt.Time,
	}
	if row.EditedAt.Valid {
		t := row.EditedAt.Time
		p.EditedAt = &t
	}
	return p
}

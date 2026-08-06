package postgres

import (
	"context"
	"errors"
	"time"

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
	return postFromRow(row.Post, row.AuthorDisplayName, row.AuthorAvatarUrl), nil
}

func (r *PostRepo) ListPosts(ctx context.Context, cursor string, limit int) ([]*domain.Post, error) {
	if cursor == "" {
		rows, err := r.q.ListPostsFeedFirst(ctx, int32(limit))
		if err != nil {
			return nil, err
		}
		posts := make([]*domain.Post, len(rows))
		for i, row := range rows {
			posts[i] = postFromRow(row.Post, row.AuthorDisplayName, row.AuthorAvatarUrl)
		}
		return posts, nil
	}

	rows, err := r.q.ListPostsFeed(ctx, ListPostsFeedParams{
		ID:    cursor,
		Limit: int32(limit),
	})
	if err != nil {
		return nil, err
	}
	posts := make([]*domain.Post, len(rows))
	for i, row := range rows {
		posts[i] = postFromRow(row.Post, row.AuthorDisplayName, row.AuthorAvatarUrl)
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
		posts[i] = postFromRow(row.Post, row.AuthorDisplayName, row.AuthorAvatarUrl)
	}
	return posts, nil
}

func (r *PostRepo) CountPostsByAuthorSince(ctx context.Context, authorID string, since time.Time) (int64, error) {
	return r.q.CountPostsByAuthorSince(ctx, CountPostsByAuthorSinceParams{
		AuthorID:  authorID,
		CreatedAt: pgtype.Timestamptz{Time: since, Valid: true},
	})
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
	return postFromRow(row.Post, row.AuthorDisplayName, row.AuthorAvatarUrl), nil
}

// UpdatePostStatus takes a post down. removedBy is the moderator's id, or ""
// when the author deleted their own post — the column is a foreign key to
// users, so "" is stored as NULL rather than as an id nobody has.
func (r *PostRepo) UpdatePostStatus(ctx context.Context, id string, status domain.PostStatus, reason, removedBy string) error {
	return r.q.SoftDeletePost(ctx, SoftDeletePostParams{
		ID:            id,
		Status:        string(status),
		RemovalReason: reason,
		RemovedBy:     pgtype.Text{String: removedBy, Valid: removedBy != ""},
	})
}

// postFromRow maps a posts row and its joined author columns onto the domain
// type. Every query that returns a post selects the same shape via
// sqlc.embed(p), so one converter serves the feed, the profile listing, the
// single-post read and the edit response — and a new field on domain.Post is a
// one-line change rather than five.
func postFromRow(row Post, authorDisplayName, authorAvatarURL string) *domain.Post {
	p := &domain.Post{
		ID:                row.ID,
		AuthorID:          row.AuthorID,
		Body:              row.Body,
		ImagePath:         row.ImagePath,
		Status:            domain.PostStatus(row.Status),
		RemovalReason:     row.RemovalReason,
		RemovedBy:         row.RemovedBy.String,
		CreatedAt:         row.CreatedAt.Time,
		AuthorDisplayName: authorDisplayName,
		AuthorAvatarURL:   authorAvatarURL,
	}
	if row.EditedAt.Valid {
		t := row.EditedAt.Time
		p.EditedAt = &t
	}
	return p
}

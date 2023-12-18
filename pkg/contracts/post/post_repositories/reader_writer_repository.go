package post_repositories

import (
	"context"
	"github.com/fireynis/the-bell-api/pkg/models"
)

type ReaderWriterRepository interface {
	Reader
	Writer
}

type Reader interface {
	Get(ctx context.Context, post models.Post) (models.Post, error)
	Query(ctx context.Context, query models.PagedPosts) (models.PagedPosts, error)
}

type Writer interface {
	Create(ctx context.Context, post models.Post) (models.Post, error)
	Update(ctx context.Context, post models.Post) (models.Post, error)
	Delete(ctx context.Context, post models.Post) error
}

package post

import (
	"context"
)

type ReaderWriterRepository interface {
	Reader
	Writer
}

type Reader interface {
	Get(ctx context.Context, post Post) (Post, error)
	GetMany(ctx context.Context, post Post) ([]Post, error)
}

type Writer interface {
	Create(ctx context.Context, post Post) (Post, error)
	Update(ctx context.Context, post Post) (Post, error)
	Delete(ctx context.Context, post Post) error
}

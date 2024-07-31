package post

import "context"

type ReaderWriterService interface {
	ReaderService
	WriterService
}

type ReaderService interface {
	Get(ctx context.Context, post Post) (Post, error)
	GetMany(ctx context.Context, post Post) ([]Post, error)
}

type WriterService interface {
	Create(ctx context.Context, post Post) (Post, error)
	Update(ctx context.Context, post Post) (Post, error)
	Delete(ctx context.Context, post Post) error
}

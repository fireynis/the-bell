package post_services

import (
	"context"
	"github.com/fireynis/the-bell-api/pkg/models"
)

type ReaderService interface {
	AdminReaderService
	UserReaderService
	CityReaderService
}

type WriterService interface {
	AdminWriterService
	UserWriterService
}

type ReaderWriterService interface {
	ReaderService
	WriterService
}

type AdminReaderService interface {
	Get(ctx context.Context, post models.Post) (models.Post, error)
	Query(ctx context.Context, query models.PagedPosts) (models.PagedPosts, error)
}

type UserReaderService interface {
	GetForUser(ctx context.Context, query models.PagedPosts) (models.PagedPosts, error)
}

type CityReaderService interface {
	GetForCity(ctx context.Context, query models.PagedPosts) (models.PagedPosts, error)
}

type AdminWriterService interface {
	Save(ctx context.Context, post models.Post) (models.Post, error)
}

type UserWriterService interface {
	SaveForUser(ctx context.Context, post models.Post) (models.Post, error)
}

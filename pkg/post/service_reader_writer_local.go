package post

import (
	"context"

	"github.com/fireynis/the-bell-api/pkg/consts"
	"github.com/fireynis/the-bell-api/pkg/errors"
)

type LocalReaderWriterService struct {
	PostService ReaderWriterRepository
}

func (l *LocalReaderWriterService) Get(ctx context.Context, post Post) (Post, error) {
	return l.PostService.Get(ctx, post)
}

func (l *LocalReaderWriterService) GetMany(ctx context.Context, post Post) ([]Post, error) {
	return l.PostService.GetMany(ctx, post)
}

func (l *LocalReaderWriterService) Create(ctx context.Context, post Post) (Post, error) {
	if ctx.Value(consts.ContextUserID) == nil {
		return Post{}, errors.ErrNoUser
	}
	post.UserID = ctx.Value("user").(string)
	if err := post.Validate(); err != nil {
		return Post{}, err
	}
	return l.PostService.Create(ctx, post)
}

func (l *LocalReaderWriterService) Update(ctx context.Context, post Post) (Post, error) {
	//TODO implement me
	panic("implement me")
}

func (l *LocalReaderWriterService) Delete(ctx context.Context, post Post) error {
	//TODO implement me
	panic("implement me")
}

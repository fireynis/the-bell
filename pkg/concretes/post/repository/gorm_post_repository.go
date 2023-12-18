package repository

import (
	"context"
	"errors"
	"github.com/fireynis/the-bell-api/pkg/models"
	"gorm.io/gorm"
)

type GormPostRepository struct {
	db *gorm.DB
}

func (g *GormPostRepository) Get(ctx context.Context, post models.Post) (models.Post, error) {
	result := g.db.WithContext(ctx).First(&post, "id = ?", post.ID)
	return post, g.parseError(result.Error)
}

func (g *GormPostRepository) Query(ctx context.Context, query models.PagedPosts) (models.PagedPosts, error) {
	//TODO implement me
	panic("implement me")
}

func (g *GormPostRepository) Create(ctx context.Context, post models.Post) (models.Post, error) {
	result := g.db.WithContext(ctx).Create(&post)
	return post, g.parseError(result.Error)
}

func (g *GormPostRepository) Update(ctx context.Context, post models.Post) (models.Post, error) {
	//TODO implement me
	panic("implement me")
}

func (g *GormPostRepository) Delete(ctx context.Context, post models.Post) error {
	//TODO implement me
	panic("implement me")
}

func (g *GormPostRepository) parseError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.ErrNotFound
	}
	return models.ErrUnexpected
}

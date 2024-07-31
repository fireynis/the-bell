package post

import (
	"context"
	"github.com/fireynis/the-bell-api/pkg/errors"
	"gorm.io/gorm"
)

type GormPostRepository struct {
	db *gorm.DB
}

func (g *GormPostRepository) Get(ctx context.Context, post Post) (Post, error) {
	result := g.db.WithContext(ctx).First(&post)
	return post, errors.ParseError(result.Error)
}

func (g *GormPostRepository) GetMany(ctx context.Context, post Post) ([]Post, error) {
	posts := make([]Post, 0)
	results := g.db.WithContext(ctx).Where("id >= ?", post.ID).Limit(10).Find(&posts)
	if results.Error != nil {
		return nil, errors.ParseError(results.Error)
	}
	return posts, nil
}

func (g *GormPostRepository) Create(ctx context.Context, post Post) (Post, error) {
	result := g.db.WithContext(ctx).Create(&post)
	return post, errors.ParseError(result.Error)
}

func (g *GormPostRepository) Update(ctx context.Context, post Post) (Post, error) {
	//TODO implement me
	panic("implement me")
}

func (g *GormPostRepository) Delete(ctx context.Context, post Post) error {
	//TODO implement me
	panic("implement me")
}

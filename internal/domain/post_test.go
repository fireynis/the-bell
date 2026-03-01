package domain_test

import (
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

func TestPost_CanEdit(t *testing.T) {
	now := time.Now()

	t.Run("author within window", func(t *testing.T) {
		post := domain.Post{
			AuthorID:  "user-1",
			Status:    domain.PostVisible,
			CreatedAt: now.Add(-10 * time.Minute),
		}
		if !post.CanEdit("user-1", now) {
			t.Error("author should be able to edit within window")
		}
	})

	t.Run("non-author rejected", func(t *testing.T) {
		post := domain.Post{
			AuthorID:  "user-1",
			Status:    domain.PostVisible,
			CreatedAt: now.Add(-10 * time.Minute),
		}
		if post.CanEdit("user-2", now) {
			t.Error("non-author should not be able to edit")
		}
	})

	t.Run("past window rejected", func(t *testing.T) {
		post := domain.Post{
			AuthorID:  "user-1",
			Status:    domain.PostVisible,
			CreatedAt: now.Add(-20 * time.Minute),
		}
		if post.CanEdit("user-1", now) {
			t.Error("should not edit after 15 min window")
		}
	})

	t.Run("exactly at 15 min boundary", func(t *testing.T) {
		post := domain.Post{
			AuthorID:  "user-1",
			Status:    domain.PostVisible,
			CreatedAt: now.Add(-15 * time.Minute),
		}
		if !post.CanEdit("user-1", now) {
			t.Error("should be able to edit at exactly 15 min boundary")
		}
	})

	t.Run("removed by mod rejected", func(t *testing.T) {
		post := domain.Post{
			AuthorID:  "user-1",
			Status:    domain.PostRemovedByMod,
			CreatedAt: now.Add(-5 * time.Minute),
		}
		if post.CanEdit("user-1", now) {
			t.Error("should not edit removed post")
		}
	})

	t.Run("removed by author rejected", func(t *testing.T) {
		post := domain.Post{
			AuthorID:  "user-1",
			Status:    domain.PostRemovedByAuthor,
			CreatedAt: now.Add(-5 * time.Minute),
		}
		if post.CanEdit("user-1", now) {
			t.Error("should not edit self-removed post")
		}
	})
}

package domain_test

import (
	"encoding/json"
	"strings"
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

// RemovalReason is a moderator's private note. It rides on the same struct the
// public read paths return, so the guarantee that it cannot be serialized
// belongs next to the struct rather than in whichever handler happens to be
// the current exposure route.
func TestPost_RemovalReasonIsNeverSerialized(t *testing.T) {
	post := domain.Post{
		ID:            "post-1",
		AuthorID:      "author-1",
		Body:          "the post body",
		Status:        domain.PostRemovedByMod,
		RemovalReason: "harassment of another member; third strike",
		CreatedAt:     time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
	}

	encoded, err := json.Marshal(post)
	if err != nil {
		t.Fatalf("marshalling post: %v", err)
	}
	body := string(encoded)

	if strings.Contains(body, "removal_reason") {
		t.Errorf("serialized post carries a removal_reason key: %s", body)
	}
	if strings.Contains(body, "harassment") {
		t.Errorf("serialized post leaked the moderator's note: %s", body)
	}

	// The rest of the post must still be there — the field is hidden, not the
	// post.
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("post is not valid JSON: %v", err)
	}
	if decoded["body"] != "the post body" {
		t.Errorf("body = %v, want it preserved", decoded["body"])
	}
}

// The field is unexported to JSON, not dropped from the type: the repository
// still reads it and a future moderator-facing response type still needs it.
func TestPost_RemovalReasonSurvivesInMemory(t *testing.T) {
	post := domain.Post{RemovalReason: "off topic"}

	if post.RemovalReason != "off topic" {
		t.Errorf("RemovalReason = %q, want it readable in Go", post.RemovalReason)
	}
}

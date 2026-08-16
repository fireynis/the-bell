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

// The image description is part of the post on the wire, always — an absent
// key would reach a client as `undefined` and drop the alt attribute entirely,
// which makes a screen reader read the image's filename instead of staying
// silent. So no omitempty, and a post with no image sends "".
func TestPost_AltTextIsAlwaysSerialized(t *testing.T) {
	// No image, no description: the emptiest a post gets.
	encoded, err := json.Marshal(domain.Post{ID: "post-1", AuthorID: "author-1", Body: "text only"})
	if err != nil {
		t.Fatalf("marshalling post: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("post is not valid JSON: %v", err)
	}
	alt, present := decoded["alt_text"]
	if !present {
		t.Fatalf("serialized post carries no alt_text key: %s", encoded)
	}
	if alt != "" {
		t.Errorf("alt_text = %v, want the empty string", alt)
	}
}

// Why the feed cache needs no migration of its own.
//
// internal/cache/feed.go stores whole marshalled domain.Post JSON in a Redis
// sorted set under a 60-second TTL, and reads it back with json.Unmarshal. Any
// entry written before this field existed therefore has no alt_text key — and
// this is what happens when one is read by the new code: the field decodes to
// "", the same value an undescribed image has, and the post renders alt="".
//
// That makes adding the field backward-compatible by construction, so nothing
// flushes the cache on deploy. The worst case is an image described in the
// sixty seconds after rollout whose cached copy predates the description, and
// it cannot happen: a description can only be written after the migration, and
// a post created after it is appended to the cache with the new marshal.
func TestPost_CachedEntriesWithoutAltTextDecodeToEmpty(t *testing.T) {
	// A post exactly as the feed cache marshalled it before this field existed.
	preAltText := `{"id":"post-1","author_id":"author-1","body":"look at this",` +
		`"image_path":"/uploads/heron.jpg","status":"visible",` +
		`"created_at":"2026-03-01T12:00:00Z","author_display_name":"Ada"}`

	var post domain.Post
	if err := json.Unmarshal([]byte(preAltText), &post); err != nil {
		t.Fatalf("a cached entry from before alt_text no longer decodes: %v", err)
	}

	if post.AltText != "" {
		t.Errorf("AltText = %q, want the empty string", post.AltText)
	}
	// And the rest of the entry survives, so an old cache page is still a usable
	// page rather than a set of blank posts.
	if post.Body != "look at this" || post.ImagePath != "/uploads/heron.jpg" {
		t.Errorf("decoded post = %+v, want the cached fields preserved", post)
	}
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

// RemovedBy names the moderator who took a post down. It is moderation
// metadata, exactly like RemovalReason, and rides on the same struct the public
// read paths return — so it gets the same guarantee rather than a second
// convention. Leaking it would tell any reader holding a post id which
// moderator handled the case.
func TestPost_RemovedByIsNeverSerialized(t *testing.T) {
	post := domain.Post{
		ID:            "post-1",
		AuthorID:      "author-1",
		Body:          "the post body",
		Status:        domain.PostRemovedByMod,
		RemovalReason: "off topic",
		RemovedBy:     "moderator-42",
		CreatedAt:     time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
	}

	encoded, err := json.Marshal(post)
	if err != nil {
		t.Fatalf("marshalling post: %v", err)
	}

	// Checked as a decoded key rather than a substring: the status value of a
	// moderator-removed post is literally "removed_by_mod", so a substring
	// search for "removed_by" matches every such post whether or not the field
	// leaked, and would pass for the wrong reason.
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("post is not valid JSON: %v", err)
	}
	if _, present := decoded["removed_by"]; present {
		t.Errorf("serialized post carries a removed_by key: %s", encoded)
	}
	if strings.Contains(string(encoded), "moderator-42") {
		t.Errorf("serialized post named the moderator: %s", encoded)
	}
}

// Hidden from JSON, not dropped from the type: the repository reads it back and
// a moderator-facing response type would need it.
func TestPost_RemovedBySurvivesInMemory(t *testing.T) {
	post := domain.Post{RemovedBy: "moderator-42"}

	if post.RemovedBy != "moderator-42" {
		t.Errorf("RemovedBy = %q, want it readable in Go", post.RemovedBy)
	}
}

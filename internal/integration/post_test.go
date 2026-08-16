//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"image"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/testsupport"
)

func TestPostLifecycle(t *testing.T) {
	pool := testsupport.TestDB(t)

	// Create a test user with member role and sufficient trust to post.
	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("poster"), domain.RoleMember, 80.0)
	srv := testServer(t, pool, user)
	handler := srv.Handler()

	var createdPostID string

	t.Run("create post", func(t *testing.T) {
		body := `{"body":"Hello from integration test!"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}

		var post domain.Post
		if err := json.NewDecoder(w.Body).Decode(&post); err != nil {
			t.Fatalf("decoding response: %v", err)
		}

		if post.Body != "Hello from integration test!" {
			t.Errorf("expected body 'Hello from integration test!', got %q", post.Body)
		}
		if post.AuthorID != user.ID {
			t.Errorf("expected author_id %q, got %q", user.ID, post.AuthorID)
		}
		if post.Status != domain.PostVisible {
			t.Errorf("expected status 'visible', got %q", post.Status)
		}

		createdPostID = post.ID
	})

	t.Run("list feed contains post", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Posts      []domain.Post `json:"posts"`
			NextCursor string        `json:"next_cursor"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decoding response: %v", err)
		}

		if len(resp.Posts) == 0 {
			t.Fatal("expected at least one post in feed")
		}

		found := false
		for _, p := range resp.Posts {
			if p.ID == createdPostID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("created post %s not found in feed", createdPostID)
		}
	})

	t.Run("get by ID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/posts/"+createdPostID, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var post domain.Post
		if err := json.NewDecoder(w.Body).Decode(&post); err != nil {
			t.Fatalf("decoding response: %v", err)
		}

		if post.ID != createdPostID {
			t.Errorf("expected post ID %q, got %q", createdPostID, post.ID)
		}
	})

	t.Run("update post", func(t *testing.T) {
		body := `{"body":"Updated body from integration test!"}`
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/posts/"+createdPostID, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var post domain.Post
		if err := json.NewDecoder(w.Body).Decode(&post); err != nil {
			t.Fatalf("decoding response: %v", err)
		}

		if post.Body != "Updated body from integration test!" {
			t.Errorf("expected updated body, got %q", post.Body)
		}
	})

	t.Run("delete post", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/posts/"+createdPostID, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("deleted post not in feed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Posts []domain.Post `json:"posts"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decoding response: %v", err)
		}

		for _, p := range resp.Posts {
			if p.ID == createdPostID {
				t.Errorf("deleted post %s should not appear in feed", createdPostID)
			}
		}
	})
}

func TestPostPagination(t *testing.T) {
	pool := testsupport.TestDB(t)

	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("paginator"), domain.RoleMember, 80.0)
	srv := testServer(t, pool, user)
	handler := srv.Handler()

	// Create 5 posts.
	postIDs := make([]string, 5)
	for i := 0; i < 5; i++ {
		body, _ := json.Marshal(map[string]string{"body": "Pagination post " + string(rune('A'+i))})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("creating post %d: expected 201, got %d: %s", i, w.Code, w.Body.String())
		}

		var post domain.Post
		if err := json.NewDecoder(w.Body).Decode(&post); err != nil {
			t.Fatalf("decoding post %d: %v", i, err)
		}
		postIDs[i] = post.ID
	}

	t.Run("first page", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/posts?limit=3", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Posts      []domain.Post `json:"posts"`
			NextCursor string        `json:"next_cursor"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decoding response: %v", err)
		}

		if len(resp.Posts) != 3 {
			t.Fatalf("expected 3 posts, got %d", len(resp.Posts))
		}

		if resp.NextCursor == "" {
			t.Error("expected next_cursor to be set")
		}
	})

	t.Run("second page with cursor", func(t *testing.T) {
		// Get first page.
		req := httptest.NewRequest(http.MethodGet, "/api/v1/posts?limit=3", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		var firstPage struct {
			Posts      []domain.Post `json:"posts"`
			NextCursor string        `json:"next_cursor"`
		}
		if err := json.NewDecoder(w.Body).Decode(&firstPage); err != nil {
			t.Fatalf("decoding first page: %v", err)
		}

		// Get second page using cursor.
		req = httptest.NewRequest(http.MethodGet, "/api/v1/posts?limit=3&cursor="+firstPage.NextCursor, nil)
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var secondPage struct {
			Posts      []domain.Post `json:"posts"`
			NextCursor string        `json:"next_cursor"`
		}
		if err := json.NewDecoder(w.Body).Decode(&secondPage); err != nil {
			t.Fatalf("decoding second page: %v", err)
		}

		if len(secondPage.Posts) != 2 {
			t.Fatalf("expected 2 posts on second page, got %d", len(secondPage.Posts))
		}

		// Ensure no overlap between pages.
		firstIDs := map[string]bool{}
		for _, p := range firstPage.Posts {
			firstIDs[p.ID] = true
		}
		for _, p := range secondPage.Posts {
			if firstIDs[p.ID] {
				t.Errorf("post %s appears on both pages", p.ID)
			}
		}
	})
}

func TestPostValidation(t *testing.T) {
	pool := testsupport.TestDB(t)

	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("validator"), domain.RoleMember, 80.0)
	srv := testServer(t, pool, user)
	handler := srv.Handler()

	t.Run("empty body rejected", func(t *testing.T) {
		body := `{"body":""}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("not found for nonexistent post", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/posts/nonexistent-id", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestPostAuthorizationLowTrust(t *testing.T) {
	pool := testsupport.TestDB(t)

	// User with trust below posting threshold (30.0).
	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("lowtrust"), domain.RoleMember, 20.0)
	srv := testServer(t, pool, user)
	handler := srv.Handler()

	body := `{"body":"Should be rejected"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for low-trust user, got %d: %s", w.Code, w.Body.String())
	}
}

// The image-description path end to end: a real multipart upload against the
// wired server, a real column, and the four reads that have to carry it back.
//
// The unit tests cover the rules; what only a full stack can show is that the
// description survives the round trip through Postgres and the feed cache, and
// that a PATCH which never mentions it leaves it alone once a real UPDATE is
// doing the writing.
func TestPostAltTextLifecycle(t *testing.T) {
	pool := testsupport.TestDB(t)

	const alt = "A heron standing on the frozen millpond"
	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("describer"), domain.RoleMember, 80.0)
	srv := testServer(t, pool, user)
	handler := srv.Handler()

	var postID string

	t.Run("create with an image and a description", func(t *testing.T) {
		req := multipartPost(t, "Look at this", alt, testJPEG(t))
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}

		var post domain.Post
		if err := json.NewDecoder(w.Body).Decode(&post); err != nil {
			t.Fatalf("decoding response: %v", err)
		}
		if post.ImagePath == "" {
			t.Fatal("no image_path on a post created with an image")
		}
		if post.AltText != alt {
			t.Errorf("alt_text = %q, want %q", post.AltText, alt)
		}
		postID = post.ID
	})

	t.Run("description comes back on every read", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/posts/"+postID, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var single domain.Post
		if err := json.NewDecoder(w.Body).Decode(&single); err != nil {
			t.Fatalf("decoding single post: %v", err)
		}
		if single.AltText != alt {
			t.Errorf("single read alt_text = %q, want %q", single.AltText, alt)
		}

		for _, path := range []string{"/api/v1/posts", "/api/v1/users/" + user.ID + "/posts"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("%s: expected 200, got %d: %s", path, w.Code, w.Body.String())
			}
			var resp struct {
				Posts []domain.Post `json:"posts"`
			}
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("%s: decoding response: %v", path, err)
			}
			found := false
			for _, p := range resp.Posts {
				if p.ID != postID {
					continue
				}
				found = true
				if p.AltText != alt {
					t.Errorf("%s: alt_text = %q, want %q", path, p.AltText, alt)
				}
			}
			if !found {
				t.Errorf("%s: created post %s missing from the response", path, postID)
			}
		}
	})

	t.Run("editing the body leaves the description alone", func(t *testing.T) {
		body := `{"body":"Look at this heron"}`
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/posts/"+postID, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var post domain.Post
		if err := json.NewDecoder(w.Body).Decode(&post); err != nil {
			t.Fatalf("decoding response: %v", err)
		}
		if post.AltText != alt {
			t.Errorf("alt_text = %q after a body-only edit, want %q", post.AltText, alt)
		}
	})

	t.Run("sending an empty description clears it", func(t *testing.T) {
		body := `{"body":"Look at this heron","alt_text":""}`
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/posts/"+postID, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var post domain.Post
		if err := json.NewDecoder(w.Body).Decode(&post); err != nil {
			t.Fatalf("decoding response: %v", err)
		}
		if post.AltText != "" {
			t.Errorf("alt_text = %q, want it cleared", post.AltText)
		}
	})

	t.Run("a description with no image is refused", func(t *testing.T) {
		req := multipartPost(t, "Text only", alt, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// multipartPost builds a POST /api/v1/posts carrying the compose form's fields.
// A nil image writes no file part, which is what the composer sends for a
// text-only post.
func multipartPost(t *testing.T, body, altText string, image []byte) *http.Request {
	t.Helper()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("body", body); err != nil {
		t.Fatalf("writing body field: %v", err)
	}
	if err := w.WriteField("alt_text", altText); err != nil {
		t.Fatalf("writing alt_text field: %v", err)
	}
	if image != nil {
		part, err := w.CreateFormFile("image", "photo.jpg")
		if err != nil {
			t.Fatalf("creating image part: %v", err)
		}
		if _, err := part.Write(image); err != nil {
			t.Fatalf("writing image data: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

// testJPEG is a one-pixel JPEG, which is enough for the magic-byte check the
// upload path performs.
func testJPEG(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil); err != nil {
		t.Fatalf("encoding jpeg: %v", err)
	}
	return buf.Bytes()
}

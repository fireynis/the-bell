package handler_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/handler"
	"github.com/fireynis/the-bell/internal/storage"
)

// --- mock storage ---

type mockStorage struct {
	saved map[string][]byte
}

func newMockStorage() *mockStorage {
	return &mockStorage{saved: make(map[string][]byte)}
}

func (m *mockStorage) Save(_ context.Context, filename string, data io.Reader) (string, error) {
	b, err := io.ReadAll(data)
	if err != nil {
		return "", err
	}
	m.saved[filename] = b
	return filename, nil
}

func (m *mockStorage) URL(path string) string {
	return "/uploads/" + path
}

// Ensure mockStorage implements storage.Storage at compile time.
var _ storage.Storage = (*mockStorage)(nil)

// --- helpers to build valid image bytes ---

func makeJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encoding jpeg: %v", err)
	}
	return buf.Bytes()
}

func makePNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding png: %v", err)
	}
	return buf.Bytes()
}

func makeWebP() []byte {
	// Minimal WebP file: RIFF header + WEBP + VP8 chunk.
	header := []byte("RIFF")
	size := make([]byte, 4)
	binary.LittleEndian.PutUint32(size, 20)
	webp := []byte("WEBP")
	vp8 := []byte("VP8 ")
	chunkSize := make([]byte, 4)
	binary.LittleEndian.PutUint32(chunkSize, 4)
	payload := make([]byte, 4)

	var buf bytes.Buffer
	buf.Write(header)
	buf.Write(size)
	buf.Write(webp)
	buf.Write(vp8)
	buf.Write(chunkSize)
	buf.Write(payload)
	return buf.Bytes()
}

func buildMultipartRequest(t *testing.T, bodyText string, imageData []byte, imageFilename string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	if bodyText != "" {
		if err := w.WriteField("body", bodyText); err != nil {
			t.Fatalf("writing body field: %v", err)
		}
	}

	if imageData != nil {
		part, err := w.CreateFormFile("image", imageFilename)
		if err != nil {
			t.Fatalf("creating image part: %v", err)
		}
		if _, err := part.Write(imageData); err != nil {
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

// --- multipart upload tests ---

func TestPostHandler_Create_MultipartJPEG(t *testing.T) {
	repo := newMockPostRepo()
	svc := newTestPostService(repo)
	store := newMockStorage()
	h := handler.NewPostHandler(svc, handler.WithStorage(store))

	imgData := makeJPEG(t)
	req := buildMultipartRequest(t, "Hello with image", imgData, "photo.jpg")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var post domain.Post
	decodeBody(t, rec, &post)
	if post.Body != "Hello with image" {
		t.Errorf("body = %q, want %q", post.Body, "Hello with image")
	}
	if post.ImagePath == "" {
		t.Error("expected image_path to be set")
	}
	if len(store.saved) != 1 {
		t.Errorf("expected 1 saved file, got %d", len(store.saved))
	}
}

func TestPostHandler_Create_MultipartPNG(t *testing.T) {
	repo := newMockPostRepo()
	svc := newTestPostService(repo)
	store := newMockStorage()
	h := handler.NewPostHandler(svc, handler.WithStorage(store))

	imgData := makePNG(t)
	req := buildMultipartRequest(t, "PNG post", imgData, "photo.png")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

func TestPostHandler_Create_MultipartWebP(t *testing.T) {
	repo := newMockPostRepo()
	svc := newTestPostService(repo)
	store := newMockStorage()
	h := handler.NewPostHandler(svc, handler.WithStorage(store))

	imgData := makeWebP()
	req := buildMultipartRequest(t, "WebP post", imgData, "photo.webp")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

func TestPostHandler_Create_MultipartNoImage(t *testing.T) {
	repo := newMockPostRepo()
	svc := newTestPostService(repo)
	store := newMockStorage()
	h := handler.NewPostHandler(svc, handler.WithStorage(store))

	req := buildMultipartRequest(t, "Text only", nil, "")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var post domain.Post
	decodeBody(t, rec, &post)
	if post.ImagePath != "" {
		t.Errorf("expected empty image_path, got %q", post.ImagePath)
	}
}

func TestPostHandler_Create_MultipartUnsupportedType(t *testing.T) {
	repo := newMockPostRepo()
	svc := newTestPostService(repo)
	store := newMockStorage()
	h := handler.NewPostHandler(svc, handler.WithStorage(store))

	// GIF magic bytes
	gifData := []byte("GIF89a" + strings.Repeat("\x00", 100))
	req := buildMultipartRequest(t, "GIF post", gifData, "anim.gif")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

// A storage backend that is full, unwritable, or unreachable must not produce a
// post whose image_path points at a file that was never written.
func TestPostHandler_Create_StorageFailure(t *testing.T) {
	repo := newMockPostRepo()
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc, handler.WithStorage(&failingStorage{}))

	req := buildMultipartRequest(t, "Hello with image", makeJPEG(t), "photo.jpg")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if len(repo.posts) != 0 {
		t.Errorf("%d posts were created despite the upload failing", len(repo.posts))
	}
}

type failingStorage struct{}

func (*failingStorage) Save(context.Context, string, io.Reader) (string, error) {
	return "", errors.New("no space left on device")
}

func (*failingStorage) URL(path string) string { return "/uploads/" + path }

// An image upload against a handler with no storage configured is a
// misconfiguration, and it must fail rather than quietly dropping the image and
// creating a text-only post.
func TestPostHandler_Create_MultipartImageWithoutStorage(t *testing.T) {
	repo := newMockPostRepo()
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc) // no WithStorage

	req := buildMultipartRequest(t, "Hello with image", makeJPEG(t), "photo.jpg")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if len(repo.posts) != 0 {
		t.Errorf("%d posts were created, want the request rejected outright", len(repo.posts))
	}
}

// The saved filename is generated, never taken from the client, so two uploads
// claiming the same name cannot overwrite each other — and a name that would
// escape the storage directory never reaches it.
func TestPostHandler_Create_StoredNameIsGenerated(t *testing.T) {
	repo := newMockPostRepo()
	store := newMockStorage()
	h := handler.NewPostHandler(newTestPostService(repo), handler.WithStorage(store))

	for _, claimed := range []string{"../../etc/passwd", "../../etc/passwd"} {
		req := buildMultipartRequest(t, "Hello", makeJPEG(t), claimed)
		req = withUser(req, testUser())
		rec := httptest.NewRecorder()

		h.Create(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
		}
	}

	if len(store.saved) != 2 {
		t.Errorf("saved %d files, want 2 distinct generated names", len(store.saved))
	}
	for name := range store.saved {
		if strings.Contains(name, "/") || strings.Contains(name, "..") {
			t.Errorf("stored name %q came from the client-supplied filename", name)
		}
		if !strings.HasSuffix(name, ".jpg") {
			t.Errorf("stored name %q does not carry the detected extension", name)
		}
	}
}

func TestPostHandler_Create_JSONStillWorks(t *testing.T) {
	repo := newMockPostRepo()
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	body := `{"body":"Hello, world!"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

package handler_test

import (
	"bytes"
	"context"
	"encoding/binary"
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

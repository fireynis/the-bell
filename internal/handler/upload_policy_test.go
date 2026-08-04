package handler

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Minimal magic-byte prefixes for each supported format, padded so that
// http.DetectContentType has enough to work with.
var (
	jpegMagic = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{0}, 32)...)
	pngMagic  = append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{0}, 32)...)
	gifMagic  = append([]byte("GIF89a"), bytes.Repeat([]byte{0}, 32)...)
)

func webpBytes() []byte {
	b := make([]byte, 0, 44)
	b = append(b, []byte("RIFF")...)
	b = append(b, 0x24, 0x00, 0x00, 0x00) // file size field
	b = append(b, []byte("WEBP")...)
	return append(b, bytes.Repeat([]byte{0}, 32)...)
}

func TestDetectImageExt_SupportedFormats(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"jpeg", jpegMagic, ".jpg"},
		{"png", pngMagic, ".png"},
		{"webp via RIFF header", webpBytes(), ".webp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := detectImageExt(tt.data)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("extension = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectImageExt_Rejects(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"plain text", []byte("this is just some text, not an image at all")},
		{"gif is not supported", gifMagic},
		{"html", []byte("<!doctype html><html><body>hi</body></html>")},
		{"elf binary", append([]byte{0x7F, 'E', 'L', 'F'}, bytes.Repeat([]byte{0}, 32)...)},
		{"truncated RIFF without WEBP", []byte("RIFF\x24\x00\x00\x00AVI ")},
		{"RIFF too short to inspect", []byte("RIFF")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ext, err := detectImageExt(tt.data)
			if err == nil {
				t.Fatalf("expected a rejection, got extension %q", ext)
			}
			if !errors.Is(err, errUnsupportedType) {
				t.Errorf("error = %v, want errUnsupportedType", err)
			}
		})
	}
}

// The stored extension comes from the file's content, never from a
// client-supplied name, so a script renamed to .jpg cannot be saved and later
// served as an image.
func TestDetectImageExt_IgnoresClaimedFilename(t *testing.T) {
	script := []byte("#!/bin/sh\necho compromised\n")

	if _, err := detectImageExt(script); !errors.Is(err, errUnsupportedType) {
		t.Errorf("a shell script named photo.jpg was accepted: err = %v", err)
	}
}

// --- parseImageUpload ---

// multipartImageRequest builds a multipart request carrying a single "image"
// part. A nil fieldName writes no file part at all.
func multipartImageRequest(t *testing.T, fieldName string, data []byte) *http.Request {
	t.Helper()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if fieldName != "" {
		part, err := w.CreateFormFile(fieldName, "photo.jpg")
		if err != nil {
			t.Fatalf("creating part: %v", err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatalf("writing part: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func TestParseImageUpload_ReturnsDataAndDetectedExtension(t *testing.T) {
	req := multipartImageRequest(t, "image", pngMagic)

	data, ext, err := parseImageUpload(req, maxImageSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ext != ".png" {
		t.Errorf("ext = %q, want %q — the extension comes from the content, not the .jpg filename", ext, ".png")
	}
	if !bytes.Equal(data, pngMagic) {
		t.Errorf("returned %d bytes, want the %d uploaded", len(data), len(pngMagic))
	}
}

// A post with no image is the common case, and it must be distinguishable from
// a failed upload: no data, no error.
func TestParseImageUpload_MissingFieldIsNotAnError(t *testing.T) {
	tests := []struct {
		name  string
		field string
	}{
		{"no file part at all", ""},
		{"a file part under a different name", "attachment"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, ext, err := parseImageUpload(multipartImageRequest(t, tt.field, pngMagic), maxImageSize)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if data != nil || ext != "" {
				t.Errorf("got (%d bytes, %q), want (nil, \"\")", len(data), ext)
			}
		})
	}
}

// The size limit is enforced from the declared part size before the bytes are
// read into memory.
func TestParseImageUpload_RejectsOversize(t *testing.T) {
	req := multipartImageRequest(t, "image", pngMagic)

	_, _, err := parseImageUpload(req, 8)
	if !errors.Is(err, errFileTooLarge) {
		t.Errorf("err = %v, want errFileTooLarge", err)
	}
}

func TestParseImageUpload_RejectsUnsupportedContent(t *testing.T) {
	req := multipartImageRequest(t, "image", gifMagic)

	if _, _, err := parseImageUpload(req, maxImageSize); !errors.Is(err, errUnsupportedType) {
		t.Errorf("err = %v, want errUnsupportedType", err)
	}
}

// A malformed request is reported rather than being mistaken for "no image
// supplied", which would silently drop the upload.
func TestParseImageUpload_MalformedRequestIsReported(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", strings.NewReader(`{"body":"hi"}`))
	req.Header.Set("Content-Type", "application/json")

	data, _, err := parseImageUpload(req, maxImageSize)
	if err == nil {
		t.Fatalf("a non-multipart request was accepted, returning %d bytes", len(data))
	}
	if errors.Is(err, errUnsupportedType) || errors.Is(err, errFileTooLarge) {
		t.Errorf("err = %v, want a distinct parse failure", err)
	}
}

func TestIsWebP(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"valid webp", webpBytes(), true},
		{"nil", nil, false},
		{"too short", []byte("RIFF\x00\x00\x00\x00WEB"), false},
		{"exactly 12 bytes", []byte("RIFF\x00\x00\x00\x00WEBP"), true},
		{"RIFF but not WEBP", []byte("RIFF\x00\x00\x00\x00AVI "), false},
		{"WEBP without RIFF", []byte("XXXX\x00\x00\x00\x00WEBP"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isWebP(tt.data); got != tt.want {
				t.Errorf("isWebP() = %v, want %v", got, tt.want)
			}
		})
	}
}

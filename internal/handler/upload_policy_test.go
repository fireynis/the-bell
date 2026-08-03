package handler

import (
	"bytes"
	"errors"
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

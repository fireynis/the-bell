package storage

import (
	"errors"
	"strings"
	"testing"
)

func TestSafeName_Accepts(t *testing.T) {
	tests := []struct {
		name     string
		filename string
	}{
		{"uuid with extension", "0195f3a1-6f2c-7c1a-9d5e-3b8a1c2d4e5f.jpg"},
		{"bare uuid", "0195f3a1-6f2c-7c1a-9d5e-3b8a1c2d4e5f"},
		{"a legitimate dot", "x.jpg"},
		{"several dots", "photo.final.v2.png"},
		{"leading dot is a hidden file, not traversal", ".hidden.webp"},
		{"three dots is an ordinary name", "..."},
		{"spaces", "my photo.jpg"},
		{"unicode", "café.png"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := safeName(tt.filename)
			if err != nil {
				t.Fatalf("safeName(%q) rejected a valid name: %v", tt.filename, err)
			}
			if got != tt.filename {
				t.Errorf("safeName(%q) = %q, want it returned unchanged", tt.filename, got)
			}
		})
	}
}

// Save joins the name onto the storage directory, so anything that could make
// the join resolve elsewhere has to be refused before it reaches the
// filesystem.
func TestSafeName_Rejects(t *testing.T) {
	tests := []struct {
		name     string
		filename string
	}{
		{"empty", ""},
		{"parent directory", ".."},
		{"current directory", "."},
		{"classic traversal", "../etc/passwd"},
		{"traversal back through a subdirectory", "a/../../b"},
		{"absolute path", "/etc/passwd"},
		{"absolute path to the storage dir itself", "/var/lib/bell/images/x.jpg"},
		{"bare separator", "/"},
		{"trailing separator", "photos/"},
		{"nested name", "photos/x.jpg"},
		{"windows traversal", `..\windows\system32`},
		{"windows absolute path", `C:\windows\system32\config`},
		{"windows separator alone", `\`},
		{"mixed separators", `a/b\c`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := safeName(tt.filename)
			if err == nil {
				t.Fatalf("safeName(%q) accepted an unsafe name, returning %q", tt.filename, got)
			}
			if !errors.Is(err, ErrUnsafeName) {
				t.Errorf("error = %v, want it to wrap ErrUnsafeName", err)
			}
			if got != "" {
				t.Errorf("rejected name still returned %q, want empty", got)
			}
		})
	}
}

// The rejection message names the offending input so an operator reading logs
// can tell which upload was refused; the sentinel is what callers branch on.
func TestSafeName_ErrorNamesTheInput(t *testing.T) {
	const bad = "../etc/passwd"

	_, err := safeName(bad)
	if err == nil {
		t.Fatal("expected a rejection")
	}
	if !strings.Contains(err.Error(), bad) {
		t.Errorf("error = %q, want it to name the rejected input %q", err, bad)
	}
}

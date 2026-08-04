package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Storage abstracts file persistence for uploads.
// Implementations may write to local disk, S3, or other backends.
type Storage interface {
	// Save persists data under the given filename and returns the
	// relative path that can be stored in the database.
	//
	// filename must name a single file — no path separators, no "."
	// or ".." — and implementations are expected to reject anything
	// else rather than trust the caller. LocalStorage returns
	// ErrUnsafeName; a name that resolves outside the backend's own
	// storage area must never be written.
	Save(ctx context.Context, filename string, data io.Reader) (path string, err error)

	// URL returns a URL (or path) that clients can use to fetch the file.
	URL(path string) string
}

// LocalStorage writes files to a directory on the local filesystem.
type LocalStorage struct {
	// basePath is the directory where files are written (e.g. /storage/the-bell/images).
	basePath string
	// urlPrefix is prepended to the filename when building a URL (e.g. /uploads/).
	urlPrefix string
}

// NewLocalStorage creates a LocalStorage that writes to basePath and serves
// files under urlPrefix.  It creates basePath if it does not exist.
func NewLocalStorage(basePath, urlPrefix string) (*LocalStorage, error) {
	if err := os.MkdirAll(basePath, 0o755); err != nil {
		return nil, fmt.Errorf("creating image storage directory: %w", err)
	}
	return &LocalStorage{
		basePath:  basePath,
		urlPrefix: urlPrefix,
	}, nil
}

// ErrUnsafeName is returned by Save when the filename could resolve to
// somewhere other than a single file directly inside the storage directory.
var ErrUnsafeName = errors.New("unsafe filename")

// safeName checks that filename names one file inside the storage directory
// and returns it unchanged.
//
// Save takes the name from its caller and the Storage contract says nothing
// about where that name came from, so a name carrying a path separator or a
// ".." element could place a write outside basePath. The guarantee belongs
// here, at the boundary, rather than in every caller.
//
// Backslashes are rejected as well. They are ordinary filename characters on
// Linux, but the contract is "one file name" — not "one file name as long as
// this OS happens not to parse it as a path" — and a name that means traversal
// on another platform has no business being accepted here.
func safeName(filename string) (string, error) {
	switch {
	case filename == "":
		return "", fmt.Errorf("%w: empty", ErrUnsafeName)
	case strings.ContainsAny(filename, `/\`):
		return "", fmt.Errorf("%w: %q contains a path separator", ErrUnsafeName, filename)
	case filename == "." || filename == "..":
		return "", fmt.Errorf("%w: %q is a directory reference", ErrUnsafeName, filename)
	}
	return filename, nil
}

// Save writes data to basePath/filename. The returned path is just the
// filename (no directory prefix) — callers store this in the database.
// It returns ErrUnsafeName if filename is not a plain file name.
func (s *LocalStorage) Save(_ context.Context, filename string, data io.Reader) (string, error) {
	name, err := safeName(filename)
	if err != nil {
		return "", err
	}

	dst := filepath.Join(s.basePath, name)

	f, err := os.Create(dst)
	if err != nil {
		return "", fmt.Errorf("creating file %s: %w", dst, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, data); err != nil {
		os.Remove(dst) // best-effort cleanup
		return "", fmt.Errorf("writing file %s: %w", dst, err)
	}

	return name, nil
}

// URL returns the public URL for a stored file.
func (s *LocalStorage) URL(path string) string {
	return s.urlPrefix + path
}

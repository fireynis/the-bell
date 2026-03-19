package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Storage abstracts file persistence for uploads.
// Implementations may write to local disk, S3, or other backends.
type Storage interface {
	// Save persists data under the given filename and returns the
	// relative path that can be stored in the database.
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

// Save writes data to basePath/filename. The returned path is just the
// filename (no directory prefix) — callers store this in the database.
func (s *LocalStorage) Save(_ context.Context, filename string, data io.Reader) (string, error) {
	dst := filepath.Join(s.basePath, filename)

	f, err := os.Create(dst)
	if err != nil {
		return "", fmt.Errorf("creating file %s: %w", dst, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, data); err != nil {
		os.Remove(dst) // best-effort cleanup
		return "", fmt.Errorf("writing file %s: %w", dst, err)
	}

	return filename, nil
}

// URL returns the public URL for a stored file.
func (s *LocalStorage) URL(path string) string {
	return s.urlPrefix + path
}

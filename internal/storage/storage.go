package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
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

	// Open returns the stored object named by path for reading. The caller
	// closes it.
	//
	// path is what Save returned, and the same one-file-name rule applies:
	// implementations reject a separator or a directory reference rather than
	// trusting the caller, so a read cannot escape the storage area any more
	// than a write can.
	//
	// A path that names nothing readable — absent, or naming a directory
	// rather than an object — returns an error satisfying
	// errors.Is(err, fs.ErrNotExist). A caller serving HTTP answers 404 for
	// that and for ErrUnsafeName alike; the two must not be distinguishable
	// from outside, or the difference enumerates the store one request at a
	// time.
	//
	// Without this the /uploads route could not read through the abstraction
	// and instead read a directory path from config, so a store writing to one
	// place and a route reading from another was not a contradiction the type
	// system could catch.
	Open(ctx context.Context, path string) (File, error)

	// URL returns a URL (or path) that clients can use to fetch the file.
	URL(path string) string
}

// File is an open stored object.
//
// The Seeker is what lets http.ServeContent answer a Range request with 206
// and derive the size without the backend reporting it; ModTime is what lets it
// answer If-Modified-Since with 304. Both matter here: uploads are served with
// a year-long Cache-Control, and 304 is how a browser renews that directive
// rather than re-downloading — see server.cacheOnSuccess.
//
// It is deliberately not fs.File. A stored object is not a directory entry:
// there is nothing to enumerate, no mode bits, and Readdir is precisely the
// method whose absence keeps /uploads/ from listing every image ever uploaded.
// ModTime is the only piece of fs.FileInfo the read path uses, so it is the
// only piece the interface asks a backend to produce.
type File interface {
	io.ReadSeekCloser

	// ModTime reports when the object was last written.
	ModTime() time.Time
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

// Open opens basePath/name for reading. It returns ErrUnsafeName if name is
// not a plain file name, and an error wrapping fs.ErrNotExist if the name does
// not resolve to a regular file.
//
// The traversal guard is the same safeName the write path uses. Reads used to
// get their own answer to that question — http.FileServer's — which meant two
// implementations of "which names may this directory serve", only one of which
// was tested by the storage package.
func (s *LocalStorage) Open(_ context.Context, name string) (File, error) {
	safe, err := safeName(name)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(filepath.Join(s.basePath, safe))
	if err != nil {
		return nil, err
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("stat %s: %w", safe, err)
	}
	if info.IsDir() {
		f.Close()
		// Reported as "not found" rather than as its own condition. A caller
		// that could tell a directory from a missing name would be able to map
		// out the store's layout, and there is nothing useful it could do with
		// the answer — this interface hands out objects, not listings.
		return nil, fmt.Errorf("open %s: %w", safe, fs.ErrNotExist)
	}

	return &localFile{f: f, modTime: info.ModTime()}, nil
}

// localFile is an *os.File narrowed to File, reporting the modification time
// read when it was opened.
//
// The time is captured once rather than re-stat'd per call: names are
// generated per upload and never reused, so an object is never rewritten, and
// a caller serving one HTTP response must see one consistent answer for the
// whole response anyway.
//
// The *os.File is a field rather than an embedded type on purpose. Embedding
// would promote Readdir, ReadDir and Name onto a value whose contract mentions
// none of them, so a caller could type-assert its way to behaviour no other
// backend can offer — and Readdir is specifically the method whose absence
// keeps /uploads/ from listing every image. The cost is that net/http can no
// longer recognise the source as an *os.File and take its sendfile path; for
// images capped at 5 MB that is not the trade worth reopening the surface for.
type localFile struct {
	f       *os.File
	modTime time.Time
}

func (f *localFile) Read(p []byte) (int, error) { return f.f.Read(p) }

func (f *localFile) Seek(offset int64, whence int) (int64, error) {
	return f.f.Seek(offset, whence)
}

func (f *localFile) Close() error { return f.f.Close() }

func (f *localFile) ModTime() time.Time { return f.modTime }

// URL returns the public URL for a stored file.
func (s *LocalStorage) URL(path string) string {
	return s.urlPrefix + path
}

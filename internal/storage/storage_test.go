package storage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/fireynis/the-bell/internal/storage"
)

func TestLocalStorage_Save(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewLocalStorage(dir, "/uploads/")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	data := []byte("fake image data")
	path, err := store.Save(context.Background(), "abc.jpg", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	if path != "abc.jpg" {
		t.Errorf("path = %q, want %q", path, "abc.jpg")
	}

	got, err := os.ReadFile(filepath.Join(dir, "abc.jpg"))
	if err != nil {
		t.Fatalf("reading saved file: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Error("saved file content does not match input")
	}
}

// The traversal guard has to hold through the exported API, not just in the
// unexported checker: nothing may be written outside the storage directory.
func TestLocalStorage_Save_RejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "images")
	store, err := storage.NewLocalStorage(sub, "/uploads/")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	names := []string{"../escaped.txt", "../../escaped.txt", "a/../../escaped.txt", "/tmp/escaped.txt", "", ".."}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			path, err := store.Save(context.Background(), name, bytes.NewReader([]byte("payload")))
			if err == nil {
				t.Fatalf("Save(%q) succeeded, returning %q", name, path)
			}
			if !errors.Is(err, storage.ErrUnsafeName) {
				t.Errorf("error = %v, want ErrUnsafeName", err)
			}
		})
	}

	// Nothing escaped into the parent of the storage directory.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "images" {
		t.Errorf("storage parent contains %v, want only the images directory", entries)
	}
}

func TestLocalStorage_Save_CreateFails(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewLocalStorage(filepath.Join(dir, "images"), "/uploads/")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	// Remove the directory out from under the store; os.Create then fails.
	if err := os.RemoveAll(filepath.Join(dir, "images")); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	if _, err := store.Save(context.Background(), "abc.jpg", bytes.NewReader([]byte("data"))); err == nil {
		t.Fatal("Save succeeded with no storage directory")
	}
}

// A read that dies partway through must not leave a truncated file behind for
// a later request to serve as a valid image.
func TestLocalStorage_Save_CopyFailsAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewLocalStorage(dir, "/uploads/")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	_, err = store.Save(context.Background(), "abc.jpg", &failingReader{})
	if err == nil {
		t.Fatal("Save succeeded with a failing reader")
	}

	if _, err := os.Stat(filepath.Join(dir, "abc.jpg")); !os.IsNotExist(err) {
		t.Errorf("partial file was left behind: stat err = %v", err)
	}
}

type failingReader struct{}

func (*failingReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

func TestNewLocalStorage_MkdirFails(t *testing.T) {
	dir := t.TempDir()

	// A regular file where the storage directory should be: MkdirAll fails.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := storage.NewLocalStorage(filepath.Join(blocker, "images"), "/uploads/"); err == nil {
		t.Fatal("NewLocalStorage succeeded with a file in the way")
	}
}

func TestLocalStorage_URL(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewLocalStorage(dir, "/uploads/")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	url := store.URL("abc.jpg")
	if url != "/uploads/abc.jpg" {
		t.Errorf("URL = %q, want %q", url, "/uploads/abc.jpg")
	}
}

func TestNewLocalStorage_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	_, err := storage.NewLocalStorage(dir, "/uploads/")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory to be created")
	}
}

func TestLocalStorage_Open(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewLocalStorage(dir, "/uploads/")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	data := []byte("fake image data")
	if _, err := store.Save(context.Background(), "abc.jpg", bytes.NewReader(data)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	f, err := store.Open(context.Background(), "abc.jpg")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("read %q, want %q", got, data)
	}
}

// Save then Open, with nothing in between knowing where the bytes went. This
// is the property that lets the /uploads route serve through the interface
// instead of reading a directory path out of config and hoping it matches.
func TestLocalStorage_SaveThenOpenRoundTrips(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewLocalStorage(dir, "/uploads/")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	data := []byte("round trip")
	path, err := store.Save(context.Background(), "0195f3a1-6f2c-7c1a-9d5e-3b8a1c2d4e5f.png", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	f, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open(%q) after Save returned it: %v", path, err)
	}
	defer f.Close()

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("read %q, want %q", got, data)
	}
}

// http.ServeContent seeks to the end to learn the size and then seeks back, so
// a File that cannot do that serves nothing. Range requests need it too.
func TestLocalStorage_Open_IsSeekable(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewLocalStorage(dir, "/uploads/")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	data := []byte("0123456789")
	if _, err := store.Save(context.Background(), "abc.jpg", bytes.NewReader(data)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	f, err := store.Open(context.Background(), "abc.jpg")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		t.Fatalf("Seek to end: %v", err)
	}
	if size != int64(len(data)) {
		t.Errorf("size = %d, want %d", size, len(data))
	}

	if _, err := f.Seek(4, io.SeekStart); err != nil {
		t.Fatalf("Seek back: %v", err)
	}
	rest, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("reading after seek: %v", err)
	}
	if string(rest) != "456789" {
		t.Errorf("read %q after seeking to 4, want %q", rest, "456789")
	}
}

// ModTime is what lets a caller answer If-Modified-Since with 304 instead of
// resending the file, which is how a browser renews the year-long
// Cache-Control on /uploads.
func TestLocalStorage_Open_ReportsModTime(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewLocalStorage(dir, "/uploads/")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	if _, err := store.Save(context.Background(), "abc.jpg", bytes.NewReader([]byte("data"))); err != nil {
		t.Fatalf("Save: %v", err)
	}

	f, err := store.Open(context.Background(), "abc.jpg")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	info, err := os.Stat(filepath.Join(dir, "abc.jpg"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !f.ModTime().Equal(info.ModTime()) {
		t.Errorf("ModTime() = %v, want %v", f.ModTime(), info.ModTime())
	}
}

// The read path must refuse exactly what the write path refuses. A store that
// will not write "../etc/passwd" but will read it has a traversal hole on the
// side nobody thought to check.
func TestLocalStorage_Open_RejectsTraversal(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte("top secret"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	store, err := storage.NewLocalStorage(filepath.Join(root, "images"), "/uploads/")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	names := []string{
		"../secret.txt",
		"../../etc/passwd",
		"a/../../secret.txt",
		"/etc/passwd",
		"",
		".",
		"..",
		"/",
		"photos/x.jpg",
		`..\windows\system32`,
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			f, err := store.Open(context.Background(), name)
			if err == nil {
				f.Close()
				t.Fatalf("Open(%q) succeeded", name)
			}
			if !errors.Is(err, storage.ErrUnsafeName) {
				t.Errorf("error = %v, want ErrUnsafeName", err)
			}
		})
	}
}

// A name that does not exist and a name that turns out to be a directory are
// the same answer, and both have to satisfy errors.Is(err, fs.ErrNotExist) —
// that is what an HTTP caller branches on to answer 404 rather than 500.
func TestLocalStorage_Open_MissingAndDirectoryAreBothNotExist(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewLocalStorage(dir, "/uploads/")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	for _, name := range []string{"nothing-here.jpg", "subdir"} {
		t.Run(name, func(t *testing.T) {
			f, err := store.Open(context.Background(), name)
			if err == nil {
				f.Close()
				t.Fatalf("Open(%q) succeeded", name)
			}
			if !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("error = %v, want it to wrap fs.ErrNotExist", err)
			}
		})
	}
}

// Nothing about the interface promises a listing, and the concrete type must
// not accidentally supply one: an *os.File for a directory would satisfy
// interface{ Readdir(int) ([]fs.FileInfo, error) } and hand any caller that
// type-asserts for it the filename of every upload.
func TestLocalStorage_Open_DoesNotHandOutDirectoryListings(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "private-photo.jpg"), []byte("bytes"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	store, err := storage.NewLocalStorage(dir, "/uploads/")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	// The only names that could reach a directory are refused before the
	// filesystem is touched; "." and "" are the ones an HTTP path produces.
	for _, name := range []string{"", ".", "/"} {
		if f, err := store.Open(context.Background(), name); err == nil {
			f.Close()
			t.Fatalf("Open(%q) opened the storage directory itself", name)
		}
	}

	// And an opened object exposes no way to enumerate its neighbours.
	if _, err := store.Save(context.Background(), "known.jpg", bytes.NewReader([]byte("x"))); err != nil {
		t.Fatalf("Save: %v", err)
	}
	f, err := store.Open(context.Background(), "known.jpg")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	if _, ok := any(f).(interface {
		ReadDir(int) ([]os.DirEntry, error)
	}); ok {
		t.Error("storage.File exposes ReadDir")
	}
}

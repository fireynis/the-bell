package storage_test

import (
	"bytes"
	"context"
	"errors"
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

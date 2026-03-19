package storage_test

import (
	"bytes"
	"context"
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

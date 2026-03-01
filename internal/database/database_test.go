package database

import (
	"context"
	"testing"
)

func TestConnect_InvalidURL(t *testing.T) {
	ctx := context.Background()
	pool, err := Connect(ctx, "postgres://invalid:5432/nonexistent?connect_timeout=1")
	if err == nil {
		pool.Close()
		t.Fatal("expected error for unreachable database, got nil")
	}
}

func TestConnect_MalformedURL(t *testing.T) {
	ctx := context.Background()
	pool, err := Connect(ctx, "not-a-url")
	if err == nil {
		pool.Close()
		t.Fatal("expected error for malformed URL, got nil")
	}
}

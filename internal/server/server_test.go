package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/config"
	"github.com/fireynis/the-bell/internal/server"
)

func newTestServer(t *testing.T) *server.Server {
	t.Helper()
	cfg := config.Config{Port: 0}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	return server.New(cfg, nil, logger)
}

func TestHealthEndpoint(t *testing.T) {
	srv := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("body status = %q, want %q", body["status"], "ok")
	}
}

func TestNotFound(t *testing.T) {
	srv := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/nonexistent")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

// A graceful shutdown must surface as http.ErrServerClosed rather than a real
// error, because main treats anything else as a crash.
func TestStartAndShutdown(t *testing.T) {
	srv := newTestServer(t)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error: %v", err)
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("Start() returned %v, want http.ErrServerClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return after Shutdown")
	}
}

func TestMethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/healthz", "application/json", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The prerequisite probes decide whether setup refuses to continue, so they are
// exercised against real listeners rather than stubs — a probe that reports
// "reachable" for something unreachable would let setup fail much later, half
// way through creating Kratos identities.

func TestCheckPostgres(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	listening := "postgres://bell@" + ln.Addr().String() + "/bell"

	// A port nothing is listening on: bind one, then release it.
	spare, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	closedAddr := spare.Addr().String()
	spare.Close()

	tests := []struct {
		name string
		dsn  string
		want bool
	}{
		{"accepting listener is reachable", listening, true},
		{"nothing listening on the port", "postgres://bell@" + closedAddr + "/bell", false},
		{"unparseable DSN", "://nope", false},
		{"unix socket host cannot be TCP-dialled", "postgres:///bell?host=/var/run/postgresql", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := checkPostgres(context.Background(), tt.dsn); got != tt.want {
				t.Errorf("checkPostgres(%q) = %v, want %v", tt.dsn, got, tt.want)
			}
		})
	}
}

// The probe honours cancellation now that it dials with a context; before, the
// caller had no way to abort a hung dial.
func TestCheckPostgres_RespectsCancelledContext(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if checkPostgres(ctx, "postgres://bell@"+ln.Addr().String()+"/bell") {
		t.Error("checkPostgres reported reachable despite a cancelled context")
	}
}

func TestCheckKratosHealth(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   bool
	}{
		{"200 is healthy", http.StatusOK, true},
		{"503 is not healthy", http.StatusServiceUnavailable, false},
		{"404 is not healthy", http.StatusNotFound, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				w.WriteHeader(tt.status)
			}))
			defer srv.Close()

			if got := checkKratosHealth(srv.URL); got != tt.want {
				t.Errorf("checkKratosHealth() = %v, want %v", got, tt.want)
			}
			if gotPath != "/health/alive" {
				t.Errorf("probed path = %q, want %q", gotPath, "/health/alive")
			}
		})
	}
}

// A trailing slash in KRATOS_ADMIN_URL must not turn the probe into a request
// for "//health/alive", which Kratos does not serve.
func TestCheckKratosHealth_TrailingSlashStillProbesTheRightPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if !checkKratosHealth(srv.URL + "/") {
		t.Error("checkKratosHealth() = false, want true")
	}
	if gotPath != "/health/alive" {
		t.Errorf("probed path = %q, want %q", gotPath, "/health/alive")
	}
}

func TestCheckKratosHealth_Unreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	if checkKratosHealth(url) {
		t.Error("checkKratosHealth() = true for a closed server, want false")
	}
}

func TestCheckRedis_UnparseableURL(t *testing.T) {
	if checkRedis(context.Background(), "not-a-redis-url") {
		t.Error("checkRedis() = true for an unparseable URL, want false")
	}
}

// Redis is optional: no URL means the graph is built without it, not an error.
func TestConnectRedis_NoURLIsNotAnError(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	client, err := connectRedis(context.Background(), "", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client != nil {
		t.Error("client is non-nil for an empty REDIS_URL, want nil")
	}
}

// A REDIS_URL that is set but broken is a misconfiguration. Silently running
// degraded would hide it until someone noticed the feed was stale.
func TestConnectRedis_BadURLIsAnError(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	if _, err := connectRedis(context.Background(), "not-a-redis-url", logger); err == nil {
		t.Error("expected an error for an unparseable REDIS_URL, got nil")
	} else if !strings.Contains(err.Error(), "REDIS_URL") {
		t.Errorf("error = %v, want it to name REDIS_URL", err)
	}
}

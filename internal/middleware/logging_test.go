package middleware_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/middleware"
)

func TestRequestLogger_LogsRequest(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.RequestLogger(logger)(inner)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse log entry: %v", err)
	}

	if entry["method"] != "GET" {
		t.Errorf("method = %v, want GET", entry["method"])
	}
	if entry["path"] != "/healthz" {
		t.Errorf("path = %v, want /healthz", entry["path"])
	}
	// JSON numbers unmarshal as float64.
	if status, ok := entry["status"].(float64); !ok || status != 200 {
		t.Errorf("status = %v, want 200", entry["status"])
	}
	if _, ok := entry["duration"]; !ok {
		t.Error("expected duration in log entry")
	}
}

func TestRequestLogger_CapturesStatusCode(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	handler := middleware.RequestLogger(logger)(inner)

	req := httptest.NewRequest(http.MethodPost, "/missing", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse log entry: %v", err)
	}

	if entry["method"] != "POST" {
		t.Errorf("method = %v, want POST", entry["method"])
	}
	if entry["path"] != "/missing" {
		t.Errorf("path = %v, want /missing", entry["path"])
	}
	if status, ok := entry["status"].(float64); !ok || status != 404 {
		t.Errorf("status = %v, want 404", entry["status"])
	}
}

func TestRequestLogger_DefaultStatus200(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	// Handler that writes body without calling WriteHeader explicitly.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	handler := middleware.RequestLogger(logger)(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse log entry: %v", err)
	}

	if status, ok := entry["status"].(float64); !ok || status != 200 {
		t.Errorf("status = %v, want 200 (implicit)", entry["status"])
	}
}

// The SSE handler extends its write deadline through http.ResponseController to
// survive the server's WriteTimeout. statusWriter embeds the ResponseWriter
// interface, which does not declare SetWriteDeadline, so without Unwrap the
// controller cannot reach the connection and every deadline extension silently
// becomes a no-op — capping SSE streams at WriteTimeout.
func TestStatusWriter_SupportsResponseController(t *testing.T) {
	var deadlineErr error

	handler := middleware.RequestLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			deadlineErr = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Minute))
		}),
	)

	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	if deadlineErr != nil {
		t.Errorf("SetWriteDeadline through RequestLogger = %v, want nil", deadlineErr)
	}
}

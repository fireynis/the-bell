package middleware_test

import (
	"bytes"
	"context"
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

// The SSE handler flushes after every frame. statusWriter must pass that
// through: if it stops being an http.Flusher, the SSE handler answers
// "streaming not supported" and real-time updates stop entirely.
func TestStatusWriter_IsAFlusher(t *testing.T) {
	var isFlusher bool

	handler := middleware.RequestLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, isFlusher = w.(http.Flusher)
		}),
	)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !isFlusher {
		t.Error("the wrapped ResponseWriter does not implement http.Flusher")
	}
}

// nonFlushingWriter is a ResponseWriter that deliberately does not implement
// http.Flusher, which is the common case: most requests are not SSE.
type nonFlushingWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *nonFlushingWriter) Header() http.Header         { return w.header }
func (w *nonFlushingWriter) Write(b []byte) (int, error) { return w.body.Write(b) }
func (w *nonFlushingWriter) WriteHeader(code int)        { w.status = code }

// countingFlusher records how many times the underlying writer was flushed.
type countingFlusher struct {
	nonFlushingWriter
	flushes int
}

func (w *countingFlusher) Flush() { w.flushes++ }

// Forwarding matters as much as implementing it: a Flush that does nothing
// leaves SSE frames sitting in the server's buffer. One handler flush must
// produce exactly one flush of the connection — no swallowing, no doubling.
func TestStatusWriter_ForwardsFlushToTheUnderlyingWriter(t *testing.T) {
	underlying := &countingFlusher{nonFlushingWriter: nonFlushingWriter{header: http.Header{}}}

	handler := middleware.RequestLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("frame"))
			w.(http.Flusher).Flush()
		}),
	)
	handler.ServeHTTP(underlying, httptest.NewRequest(http.MethodGet, "/", nil))

	if underlying.flushes != 1 {
		t.Errorf("underlying writer flushed %d times, want exactly 1", underlying.flushes)
	}
}

// The other side of the invariant. statusWriter always presents as an
// http.Flusher, so a handler will call Flush on it even when the connection
// underneath cannot flush. That must be a silent no-op: a panic here would take
// down every non-streaming request that happens to flush.
func TestStatusWriter_FlushIsASafeNoOpWhenTheWriterCannotFlush(t *testing.T) {
	underlying := &nonFlushingWriter{header: http.Header{}}

	// Guard the premise — if this fake ever gained a Flush method, the test
	// would silently stop covering the no-op path.
	if _, ok := http.ResponseWriter(underlying).(http.Flusher); ok {
		t.Fatal("the fake writer must not implement http.Flusher")
	}

	completed := false
	handler := middleware.RequestLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			f, ok := w.(http.Flusher)
			if !ok {
				t.Error("statusWriter must always present as an http.Flusher")
				return
			}
			f.Flush() // must not panic
			w.Write([]byte("ok"))
			completed = true
		}),
	)
	handler.ServeHTTP(underlying, httptest.NewRequest(http.MethodGet, "/", nil))

	if !completed {
		t.Fatal("the handler did not run to completion")
	}
	if got := underlying.body.String(); got != "ok" {
		t.Errorf("body = %q, want %q — the no-op flush disturbed the response", got, "ok")
	}
}

// End to end: bytes flushed by the handler must reach the client before the
// handler returns, which is the whole premise of the SSE stream.
func TestStatusWriter_FlushReachesTheClientBeforeTheHandlerReturns(t *testing.T) {
	release := make(chan struct{})

	srv := httptest.NewServer(middleware.RequestLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("frame\n"))
			w.(http.Flusher).Flush()
			<-release // hold the response open until the client has read
		}),
	))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})

	// The handler never returns on its own, so bound the whole exchange: a
	// Flush that no longer reaches the connection must fail here, not hang.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("response headers never arrived; the response was buffered: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })

	buf := make([]byte, len("frame\n"))
	if _, err := io.ReadFull(resp.Body, buf); err != nil {
		t.Fatalf("flushed bytes never reached the client: %v", err)
	}
	if string(buf) != "frame\n" {
		t.Errorf("read %q, want %q", buf, "frame\n")
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

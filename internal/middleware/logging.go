package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// statusWriter wraps http.ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status = code
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

// Flush delegates to the underlying ResponseWriter if it supports flushing
// (required for SSE streaming).
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap exposes the wrapped ResponseWriter to http.ResponseController.
//
// The embedded field is the http.ResponseWriter *interface*, which declares
// only Header/Write/WriteHeader, so optional methods like SetWriteDeadline are
// not promoted through it. Without this, ResponseController finds no way down
// to the real connection and every SetWriteDeadline call returns
// http.ErrNotSupported — which silently caps SSE streams at the server's
// WriteTimeout.
func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// RequestLogger returns middleware that logs each request with method, path,
// status code, and duration.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration", time.Since(start),
			)
		})
	}
}

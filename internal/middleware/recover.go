package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Recoverer converts a panic in a downstream handler into a 500 response.
//
// Without it a panic unwinds into net/http, which closes the connection with no
// response at all: the client sees a transport error rather than a status, and
// the stack goes to stderr unstructured. Handlers do panic by design in places
// (handler.mustUUIDv7), so this is a live path, not just defence in depth.
//
// http.ErrAbortHandler is the one panic value net/http defines as intentional —
// it is re-panicked so the server can drop the connection as the caller asked.
//
// If the handler already wrote a status the response is committed and only the
// log entry can be salvaged, so the body is left alone.
func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sw, tracked := w.(*statusWriter)

			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if rec == http.ErrAbortHandler {
					panic(rec)
				}

				logger.Error("panic recovered",
					"method", r.Method,
					"path", r.URL.Path,
					"panic", rec,
					"stack", string(debug.Stack()),
				)

				if tracked && sw.wrote {
					return
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error":"internal error"}`))
			}()

			next.ServeHTTP(w, r)
		})
	}
}

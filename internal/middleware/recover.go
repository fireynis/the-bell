package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/fireynis/the-bell/internal/httpjson"
)

// Recoverer converts a panic in a downstream handler into a 500 response.
//
// Without it a panic unwinds into net/http, which closes the connection with no
// response at all: the client sees a transport error rather than a status, and
// the stack goes to stderr unstructured.
//
// No handler panics by design any more — handler.mustUUIDv7 was the last one,
// and it now returns an error — so this is defence in depth against a nil
// dereference or an out-of-range index in code nobody expected to fail. That is
// the state to keep it in: a handler that reaches for Recoverer deliberately is
// putting its error handling in the outermost frame, where nothing specific to
// the request can be said about it.
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

				// Nothing can be salvaged once a status is on the wire: the
				// headers are already flushed, so writing again would append a
				// second body to a committed response rather than replace it.
				if tracked && sw.wrote {
					return
				}

				httpjson.WriteError(w, http.StatusInternalServerError, "internal error")
			}()

			next.ServeHTTP(w, r)
		})
	}
}

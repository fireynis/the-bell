package middleware_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fireynis/the-bell/internal/middleware"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRecoverer_TurnsPanicIntoInternalError(t *testing.T) {
	h := middleware.Recoverer(quietLogger())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %q", rec.Body.String())
	}
	if body["error"] != "internal error" {
		t.Errorf("error = %q, want %q", body["error"], "internal error")
	}
}

// The panic value and stack must never reach the client — they routinely carry
// internal paths and identifiers.
func TestRecoverer_DoesNotLeakPanicDetail(t *testing.T) {
	h := middleware.Recoverer(quietLogger())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("connection to db-primary-1 as appuser failed")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if strings.Contains(rec.Body.String(), "db-primary-1") {
		t.Errorf("response leaked panic detail: %q", rec.Body.String())
	}
}

func TestRecoverer_PassesThroughNormalResponses(t *testing.T) {
	h := middleware.Recoverer(quietLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		w.Write([]byte(`{"ok":true}`))
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
	if rec.Body.String() != `{"ok":true}` {
		t.Errorf("body = %q, want it untouched", rec.Body.String())
	}
}

func TestRecoverer_RecoversNonStringPanicValues(t *testing.T) {
	for _, value := range []any{42, struct{ A int }{1}, error(nil)} {
		h := middleware.Recoverer(quietLogger())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic(value)
		}))

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("panic(%v): status = %d, want 500", value, rec.Code)
		}
	}
}

// http.ErrAbortHandler is net/http's documented way to drop a connection on
// purpose, so it must keep propagating rather than becoming a 500.
func TestRecoverer_RepanicsErrAbortHandler(t *testing.T) {
	h := middleware.Recoverer(quietLogger())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		if rec := recover(); rec != http.ErrAbortHandler {
			t.Errorf("recovered %v, want ErrAbortHandler to propagate", rec)
		}
	}()

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	t.Error("expected ErrAbortHandler to propagate")
}

// A panic after the handler already wrote a status cannot be turned into a 500;
// the middleware must not corrupt the committed response.
func TestRecoverer_LeavesCommittedResponseAlone(t *testing.T) {
	h := middleware.RequestLogger(quietLogger())(
		middleware.Recoverer(quietLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"partial":`))
			panic("failed midway")
		})),
	)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want the already-committed %d", rec.Code, http.StatusOK)
	}
	if strings.Contains(rec.Body.String(), "internal error") {
		t.Errorf("appended an error body to a committed response: %q", rec.Body.String())
	}
}

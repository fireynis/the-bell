package httpjson_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/handler"
	"github.com/fireynis/the-bell/internal/httpjson"
	"github.com/fireynis/the-bell/internal/middleware"
)

func TestWrite(t *testing.T) {
	rec := httptest.NewRecorder()

	httpjson.Write(rec, http.StatusCreated, map[string]string{"key": "value"})

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
	if got := rec.Body.String(); got != `{"key":"value"}` {
		t.Errorf("body = %q, want %q", got, `{"key":"value"}`)
	}
}

// The response must not end in a newline. handler.JSON has always written its
// body with json.Marshal, and the API's consumers decode those exact bytes.
func TestWrite_HasNoTrailingNewline(t *testing.T) {
	rec := httptest.NewRecorder()

	httpjson.Write(rec, http.StatusOK, map[string]string{"status": "ok"})

	if got := rec.Body.String(); got != `{"status":"ok"}` {
		t.Errorf("body = %q, want %q with no trailing newline", got, `{"status":"ok"}`)
	}
}

// An unmarshalable value still has to produce a valid JSON error body rather
// than a half-written response.
func TestWrite_MarshalFailureWritesInternalError(t *testing.T) {
	rec := httptest.NewRecorder()

	httpjson.Write(rec, http.StatusOK, make(chan int))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if got := rec.Body.String(); got != `{"error":"internal error"}` {
		t.Errorf("body = %q, want %q", got, `{"error":"internal error"}`)
	}
}

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()

	httpjson.WriteError(rec, http.StatusBadRequest, "bad input")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got := rec.Body.String(); got != `{"error":"bad input"}` {
		t.Errorf("body = %q, want %q", got, `{"error":"bad input"}`)
	}
}

// The regression this package exists to prevent: handler and middleware used
// to have separate copies of this code and had already drifted — handler used
// json.Marshal, middleware used json.Encoder, which appends a newline. A
// client parsing an error body could not rely on the two agreeing.
//
// middleware.writeError is unexported, so drive it the way production does:
// RequireActive rejects a request with no user in its context.
func TestHandlerAndMiddlewareEmitIdenticalErrorBytes(t *testing.T) {
	const (
		status  = http.StatusUnauthorized
		message = "unauthorized"
	)

	fromHandler := httptest.NewRecorder()
	handler.Error(fromHandler, status, message)

	fromMiddleware := httptest.NewRecorder()
	rejectIfReached := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("RequireActive passed a request that had no user in context")
	})
	middleware.RequireActive(rejectIfReached).ServeHTTP(fromMiddleware, httptest.NewRequest(http.MethodGet, "/", nil))

	if fromMiddleware.Code != status {
		t.Fatalf("middleware status = %d, want %d", fromMiddleware.Code, status)
	}
	if got, want := fromMiddleware.Body.String(), fromHandler.Body.String(); got != want {
		t.Errorf("middleware body = %q, handler body = %q; they must be byte-identical", got, want)
	}
	if got, want := fromMiddleware.Header().Get("Content-Type"), fromHandler.Header().Get("Content-Type"); got != want {
		t.Errorf("middleware Content-Type = %q, handler Content-Type = %q", got, want)
	}
}

// The third writer of this shape. Recoverer builds its 500 on a panic, so it
// is driven by panicking rather than by a status check — but the bytes a
// client sees must match the other two exactly.
func TestRecovererEmitsIdenticalErrorBytes(t *testing.T) {
	fromHandler := httptest.NewRecorder()
	handler.Error(fromHandler, http.StatusInternalServerError, "internal error")

	fromMiddleware := httptest.NewRecorder()
	panicking := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	middleware.Recoverer(quiet)(panicking).ServeHTTP(fromMiddleware, httptest.NewRequest(http.MethodGet, "/", nil))

	if fromMiddleware.Code != http.StatusInternalServerError {
		t.Fatalf("recoverer status = %d, want %d", fromMiddleware.Code, http.StatusInternalServerError)
	}
	if got, want := fromMiddleware.Body.String(), fromHandler.Body.String(); got != want {
		t.Errorf("recoverer body = %q, handler body = %q; they must be byte-identical", got, want)
	}
	if got, want := fromMiddleware.Header().Get("Content-Type"), fromHandler.Header().Get("Content-Type"); got != want {
		t.Errorf("recoverer Content-Type = %q, handler Content-Type = %q", got, want)
	}
}

// Routing the recoverer through httpjson must not have cost it its
// already-committed guard: a panic after the handler wrote a status has to
// leave that response alone rather than appending a second JSON body to it.
func TestRecovererStillLeavesCommittedResponsesAlone(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := middleware.RequestLogger(quiet)(
		middleware.Recoverer(quiet)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	if got := rec.Body.String(); got != `{"partial":` {
		t.Errorf("body = %q, want the committed response untouched", got)
	}
}

// The same check on the forbidden path, which carries a different message and
// status through the identical code.
func TestHandlerAndMiddlewareEmitIdenticalForbiddenBytes(t *testing.T) {
	suspended := &domain.User{ID: "user-1", Role: domain.RoleMember, IsActive: false}

	fromHandler := httptest.NewRecorder()
	handler.Error(fromHandler, http.StatusForbidden, "account suspended")

	fromMiddleware := httptest.NewRecorder()
	rejectIfReached := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("RequireActive passed a suspended user")
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(middleware.WithUser(req.Context(), suspended))
	middleware.RequireActive(rejectIfReached).ServeHTTP(fromMiddleware, req)

	if fromMiddleware.Code != http.StatusForbidden {
		t.Fatalf("middleware status = %d, want %d", fromMiddleware.Code, http.StatusForbidden)
	}
	if got, want := fromMiddleware.Body.String(), fromHandler.Body.String(); got != want {
		t.Errorf("middleware body = %q, handler body = %q; they must be byte-identical", got, want)
	}
}

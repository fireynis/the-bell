package middleware_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
	kratos "github.com/ory/kratos-client-go"
)

// mockUserFinder implements middleware.UserFinder for tests.
type mockUserFinder struct {
	user *domain.User
	err  error
}

func (m *mockUserFinder) FindByKratosID(_ context.Context, _ string) (*domain.User, error) {
	return m.user, m.err
}

// newKratosClient returns a kratos APIClient pointing at the given base URL.
func newKratosClient(baseURL string) *kratos.APIClient {
	cfg := kratos.NewConfiguration()
	cfg.Servers = kratos.ServerConfigurations{{URL: baseURL}}
	return kratos.NewAPIClient(cfg)
}

// kratosSessionJSON returns a minimal Kratos session response with the given identity ID.
func kratosSessionJSON(identityID string) string {
	return fmt.Sprintf(`{
		"id": "session-id",
		"active": true,
		"identity": {
			"id": %q,
			"schema_id": "default",
			"schema_url": "http://kratos/schemas/default",
			"traits": {}
		}
	}`, identityID)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(nil, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

// --- KratosAuth tests ---

func TestKratosAuth_NoCookie(t *testing.T) {
	kratosServer := httptest.NewServer(http.NotFoundHandler())
	defer kratosServer.Close()

	client := newKratosClient(kratosServer.URL)
	finder := &mockUserFinder{}
	handler := middleware.KratosAuth(client, finder, testLogger())(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusUnauthorized)
	assertErrorBody(t, rec, "unauthorized")
}

func TestKratosAuth_InvalidSession(t *testing.T) {
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"code":401,"status":"Unauthorized","message":"No active session"}}`)
	}))
	defer kratosServer.Close()

	client := newKratosClient(kratosServer.URL)
	finder := &mockUserFinder{}
	handler := middleware.KratosAuth(client, finder, testLogger())(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Cookie", "ory_session=invalid")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusUnauthorized)
	assertErrorBody(t, rec, "unauthorized")
}

func TestKratosAuth_UserNotFound(t *testing.T) {
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, kratosSessionJSON("kratos-123"))
	}))
	defer kratosServer.Close()

	client := newKratosClient(kratosServer.URL)
	finder := &mockUserFinder{user: nil, err: nil}
	handler := middleware.KratosAuth(client, finder, testLogger())(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Cookie", "ory_session=valid")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusUnauthorized)
	assertErrorBody(t, rec, "user not found")
}

func TestKratosAuth_FinderError(t *testing.T) {
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, kratosSessionJSON("kratos-123"))
	}))
	defer kratosServer.Close()

	client := newKratosClient(kratosServer.URL)
	finder := &mockUserFinder{user: nil, err: fmt.Errorf("db down")}
	handler := middleware.KratosAuth(client, finder, testLogger())(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Cookie", "ory_session=valid")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusInternalServerError)
	assertErrorBody(t, rec, "internal error")
}

func TestKratosAuth_Success(t *testing.T) {
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, kratosSessionJSON("kratos-456"))
	}))
	defer kratosServer.Close()

	user := &domain.User{
		ID:               "user-1",
		KratosIdentityID: "kratos-456",
		Role:             domain.RoleMember,
		IsActive:         true,
	}

	client := newKratosClient(kratosServer.URL)
	finder := &mockUserFinder{user: user}

	var gotUser *domain.User
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := middleware.UserFromContext(r.Context())
		if ok {
			gotUser = u
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.KratosAuth(client, finder, testLogger())(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Cookie", "ory_session=valid")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
	if gotUser == nil {
		t.Fatal("expected user in context, got nil")
	}
	if gotUser.ID != "user-1" {
		t.Errorf("user ID = %q, want %q", gotUser.ID, "user-1")
	}
}

// --- RequireRole tests ---

func TestRequireRole_NoUserInContext(t *testing.T) {
	handler := middleware.RequireRole(domain.RoleMember)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusUnauthorized)
	assertErrorBody(t, rec, "unauthorized")
}

func TestRequireRole_InsufficientRole(t *testing.T) {
	tests := []struct {
		name    string
		role    domain.Role
		require domain.Role
	}{
		{"banned requires member", domain.RoleBanned, domain.RoleMember},
		{"pending requires member", domain.RolePending, domain.RoleMember},
		{"member requires moderator", domain.RoleMember, domain.RoleModerator},
		{"moderator requires council", domain.RoleModerator, domain.RoleCouncil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := &domain.User{ID: "u1", Role: tt.role, IsActive: true}
			handler := middleware.RequireRole(tt.require)(okHandler())

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			ctx := middleware.WithUser(req.Context(), user)
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assertStatus(t, rec, http.StatusForbidden)
			assertErrorBody(t, rec, "forbidden")
		})
	}
}

func TestRequireRole_SufficientRole(t *testing.T) {
	tests := []struct {
		name    string
		role    domain.Role
		require domain.Role
	}{
		{"member meets member", domain.RoleMember, domain.RoleMember},
		{"moderator meets member", domain.RoleModerator, domain.RoleMember},
		{"council meets member", domain.RoleCouncil, domain.RoleMember},
		{"council meets council", domain.RoleCouncil, domain.RoleCouncil},
		{"moderator meets moderator", domain.RoleModerator, domain.RoleModerator},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := &domain.User{ID: "u1", Role: tt.role, IsActive: true}
			handler := middleware.RequireRole(tt.require)(okHandler())

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			ctx := middleware.WithUser(req.Context(), user)
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assertStatus(t, rec, http.StatusOK)
		})
	}
}

// --- RequireActive tests ---

func TestRequireActive_NoUserInContext(t *testing.T) {
	handler := middleware.RequireActive(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusUnauthorized)
	assertErrorBody(t, rec, "unauthorized")
}

func TestRequireActive_InactiveUser(t *testing.T) {
	user := &domain.User{ID: "u1", Role: domain.RoleMember, IsActive: false}
	handler := middleware.RequireActive(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := middleware.WithUser(req.Context(), user)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusForbidden)
	assertErrorBody(t, rec, "account suspended")
}

func TestRequireActive_ActiveUser(t *testing.T) {
	user := &domain.User{ID: "u1", Role: domain.RoleMember, IsActive: true}
	handler := middleware.RequireActive(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := middleware.WithUser(req.Context(), user)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
}

// --- Context helper tests ---

func TestWithUser_RoundTrip(t *testing.T) {
	user := &domain.User{ID: "u42", Role: domain.RoleMember}
	ctx := middleware.WithUser(context.Background(), user)

	got, ok := middleware.UserFromContext(ctx)
	if !ok {
		t.Fatal("expected ok=true, got false")
	}
	if got != user {
		t.Errorf("got different user pointer; want same")
	}
}

func TestUserFromContext_Empty(t *testing.T) {
	got, ok := middleware.UserFromContext(context.Background())
	if ok {
		t.Error("expected ok=false for empty context")
	}
	if got != nil {
		t.Errorf("expected nil user, got %v", got)
	}
}

// --- helpers ---

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Errorf("status = %d, want %d", rec.Code, want)
	}
}

func assertErrorBody(t *testing.T, rec *httptest.ResponseRecorder, wantMsg string) {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}
	if body["error"] != wantMsg {
		t.Errorf("error = %q, want %q", body["error"], wantMsg)
	}
}

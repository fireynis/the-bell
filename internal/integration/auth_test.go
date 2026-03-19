//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/service"

	"github.com/go-chi/chi/v5"
	kratos "github.com/ory/kratos-client-go"
)

// TestKratosAuthValidSession tests that valid Kratos sessions result in the
// correct user being populated in the request context.
func TestKratosAuthValidSession(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()

	// Create a user in the database first.
	kratosID := uniqueKratosID("authvalid")
	q := postgres.New(pool)
	userRepo := postgres.NewUserRepo(q)
	userSvc := service.NewUserService(userRepo, nil)

	user, err := userSvc.FindOrCreate(ctx, kratosID)
	if err != nil {
		t.Fatalf("creating test user: %v", err)
	}

	// Start a mock Kratos server that returns a valid session.
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sessions/whoami" {
			session := map[string]any{
				"id":     "session-123",
				"active": true,
				"identity": map[string]any{
					"id":          kratosID,
					"schema_id":   "default",
					"schema_url":  "http://example.com/schema",
					"traits":      map[string]any{},
					"created_at":  "2024-01-01T00:00:00Z",
					"updated_at":  "2024-01-01T00:00:00Z",
					"state":       "active",
					"state_changed_at": "2024-01-01T00:00:00Z",
				},
				"created_at":        "2024-01-01T00:00:00Z",
				"updated_at":        "2024-01-01T00:00:00Z",
				"authenticated_at":  "2024-01-01T00:00:00Z",
				"issued_at":         "2024-01-01T00:00:00Z",
				"expires_at":        "2099-01-01T00:00:00Z",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(session)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(kratosServer.Close)

	// Create a real Kratos client pointing at our mock server.
	kratosCfg := kratos.NewConfiguration()
	kratosCfg.Servers = kratos.ServerConfigurations{{URL: kratosServer.URL}}
	kratosClient := kratos.NewAPIClient(kratosCfg)

	// Build a simple test handler behind the real KratosAuth middleware.
	authMW := middleware.KratosAuth(kratosClient, userSvc, slogDiscard())

	r := chi.NewRouter()
	r.Use(authMW)
	r.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		u, ok := middleware.UserFromContext(r.Context())
		if !ok {
			http.Error(w, `{"error":"no user in context"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"user_id": u.ID,
			"role":    string(u.Role),
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Cookie", "ory_kratos_session=test-session-token")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if resp["user_id"] != user.ID {
		t.Errorf("expected user_id %q, got %q", user.ID, resp["user_id"])
	}
}

// TestKratosAuthInvalidSession tests that an invalid Kratos session returns 401.
func TestKratosAuthInvalidSession(t *testing.T) {
	pool := testDB(t)

	q := postgres.New(pool)
	userRepo := postgres.NewUserRepo(q)
	userSvc := service.NewUserService(userRepo, nil)

	// Mock Kratos server that returns 401 for all sessions.
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"code":401,"message":"No valid session"}}`))
	}))
	t.Cleanup(kratosServer.Close)

	kratosCfg := kratos.NewConfiguration()
	kratosCfg.Servers = kratos.ServerConfigurations{{URL: kratosServer.URL}}
	kratosClient := kratos.NewAPIClient(kratosCfg)

	authMW := middleware.KratosAuth(kratosClient, userSvc, slogDiscard())

	r := chi.NewRouter()
	r.Use(authMW)
	r.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Cookie", "ory_kratos_session=invalid-token")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

// TestKratosAuthNoCookie tests that requests without a cookie header return 401.
func TestKratosAuthNoCookie(t *testing.T) {
	pool := testDB(t)

	q := postgres.New(pool)
	userRepo := postgres.NewUserRepo(q)
	userSvc := service.NewUserService(userRepo, nil)

	// Mock Kratos server (should not be called).
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("kratos server should not be called when no cookie is present")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(kratosServer.Close)

	kratosCfg := kratos.NewConfiguration()
	kratosCfg.Servers = kratos.ServerConfigurations{{URL: kratosServer.URL}}
	kratosClient := kratos.NewAPIClient(kratosCfg)

	authMW := middleware.KratosAuth(kratosClient, userSvc, slogDiscard())

	r := chi.NewRouter()
	r.Use(authMW)
	r.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// No cookie header set.
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

// TestKratosAuthAutoCreateUser tests that when a valid Kratos session references
// an identity not yet in the local database, FindOrCreate auto-provisions the user.
func TestKratosAuthAutoCreateUser(t *testing.T) {
	pool := testDB(t)

	q := postgres.New(pool)
	userRepo := postgres.NewUserRepo(q)
	userSvc := service.NewUserService(userRepo, nil)

	// This Kratos ID does not exist in the database yet.
	kratosID := uniqueKratosID("autocreate")

	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sessions/whoami" {
			session := map[string]any{
				"id":     "session-autocreate",
				"active": true,
				"identity": map[string]any{
					"id":          kratosID,
					"schema_id":   "default",
					"schema_url":  "http://example.com/schema",
					"traits":      map[string]any{},
					"created_at":  "2024-01-01T00:00:00Z",
					"updated_at":  "2024-01-01T00:00:00Z",
					"state":       "active",
					"state_changed_at": "2024-01-01T00:00:00Z",
				},
				"created_at":        "2024-01-01T00:00:00Z",
				"updated_at":        "2024-01-01T00:00:00Z",
				"authenticated_at":  "2024-01-01T00:00:00Z",
				"issued_at":         "2024-01-01T00:00:00Z",
				"expires_at":        "2099-01-01T00:00:00Z",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(session)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(kratosServer.Close)

	kratosCfg := kratos.NewConfiguration()
	kratosCfg.Servers = kratos.ServerConfigurations{{URL: kratosServer.URL}}
	kratosClient := kratos.NewAPIClient(kratosCfg)

	authMW := middleware.KratosAuth(kratosClient, userSvc, slogDiscard())

	r := chi.NewRouter()
	r.Use(authMW)
	r.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		u, ok := middleware.UserFromContext(r.Context())
		if !ok {
			http.Error(w, `{"error":"no user"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"user_id":    u.ID,
			"kratos_id":  u.KratosIdentityID,
			"role":       string(u.Role),
			"trust":      fmt.Sprintf("%.1f", u.TrustScore),
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Cookie", "ory_kratos_session=test-session-token")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if resp["kratos_id"] != kratosID {
		t.Errorf("expected kratos_id %q, got %q", kratosID, resp["kratos_id"])
	}

	// Auto-created users should be pending with trust 50.0.
	if resp["role"] != string(domain.RolePending) {
		t.Errorf("expected role 'pending', got %q", resp["role"])
	}
	if resp["trust"] != "50.0" {
		t.Errorf("expected trust '50.0', got %q", resp["trust"])
	}

	// Verify the user was persisted in the database.
	created, err := userRepo.GetUserByKratosID(context.Background(), kratosID)
	if err != nil {
		t.Fatalf("looking up auto-created user: %v", err)
	}
	if created.ID != resp["user_id"] {
		t.Errorf("database user ID %q doesn't match response %q", created.ID, resp["user_id"])
	}
}

// TestRequireActiveMiddleware tests that suspended users are rejected.
func TestRequireActiveMiddleware(t *testing.T) {
	pool := testDB(t)

	// Create a suspended user (is_active = false via the mock auth).
	user := testUser(t, pool, uniqueKratosID("suspended"), domain.RoleMember, 80.0)
	user.IsActive = false

	srv := testServer(t, pool, user)
	handler := srv.Handler()

	// Try to create a post (requires active user).
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for suspended user, got %d: %s", w.Code, w.Body.String())
	}
}

// TestRequireRoleMiddleware tests role-based access control.
func TestRequireRoleMiddleware(t *testing.T) {
	pool := testDB(t)

	// A regular member trying to access moderator endpoints.
	member := testUser(t, pool, uniqueKratosID("roletest-member"), domain.RoleMember, 80.0)
	srv := testServer(t, pool, member)
	handler := srv.Handler()

	// Try to access moderation queue (requires moderator role).
	req := httptest.NewRequest(http.MethodGet, "/api/v1/moderation/queue", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for member accessing moderator endpoint, got %d: %s", w.Code, w.Body.String())
	}
}

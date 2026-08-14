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

// mockUserFinder implements middleware.UserFinder for tests. It records the
// display name it was handed so tests can assert what the middleware pulled
// out of the identity's traits.
type mockUserFinder struct {
	user *domain.User
	err  error

	gotKratosID    string
	gotDisplayName string
}

func (m *mockUserFinder) FindByKratosID(_ context.Context, kratosID, displayName string) (*domain.User, error) {
	m.gotKratosID = kratosID
	m.gotDisplayName = displayName
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
	return kratosSessionJSONWithTraits(identityID, `{}`)
}

// kratosSessionJSONWithTraits is the same, with the identity's traits object
// supplied verbatim so a test can hand the middleware a schema it does not
// expect as easily as the one it does.
func kratosSessionJSONWithTraits(identityID, traits string) string {
	return fmt.Sprintf(`{
		"id": "session-id",
		"active": true,
		"identity": {
			"id": %q,
			"schema_id": "default",
			"schema_url": "http://kratos/schemas/default",
			"traits": %s
		}
	}`, identityID, traits)
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

// --- name trait extraction ---

// The trait has to reach the finder, because that is the only moment a
// newly-provisioned user can be given a name. Miss it and the council's
// approval queue lists hex ID fragments instead of people.
func TestKratosAuth_PassesNameTraitToFinder(t *testing.T) {
	tests := []struct {
		name   string
		traits string
		want   string
	}{
		{
			name:   "name trait is forwarded",
			traits: `{"email":"ada@example.com","name":"Ada Lovelace"}`,
			want:   "Ada Lovelace",
		},
		{
			name:   "surrounding whitespace is trimmed",
			traits: `{"name":"  Ada Lovelace \n"}`,
			want:   "Ada Lovelace",
		},
		// The remaining cases are all shapes a differently-configured identity
		// schema can produce. None of them may fail the request: an
		// authenticated user with no display name is a cosmetic problem, being
		// locked out is not.
		{
			name:   "absent name trait yields empty",
			traits: `{"email":"ada@example.com"}`,
			want:   "",
		},
		{
			name:   "empty traits object yields empty",
			traits: `{}`,
			want:   "",
		},
		{
			name:   "non-string name yields empty",
			traits: `{"name":{"first":"Ada","last":"Lovelace"}}`,
			want:   "",
		},
		{
			name:   "non-object traits yield empty",
			traits: `"ada@example.com"`,
			want:   "",
		},
		{
			name:   "null traits yield empty",
			traits: `null`,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, kratosSessionJSONWithTraits("kratos-789", tt.traits))
			}))
			defer kratosServer.Close()

			finder := &mockUserFinder{user: &domain.User{
				ID:               "user-1",
				KratosIdentityID: "kratos-789",
				Role:             domain.RoleMember,
				IsActive:         true,
			}}
			handler := middleware.KratosAuth(newKratosClient(kratosServer.URL), finder, testLogger())(okHandler())

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Cookie", "ory_session=valid")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assertStatus(t, rec, http.StatusOK)
			if finder.gotKratosID != "kratos-789" {
				t.Errorf("kratos id = %q, want %q", finder.gotKratosID, "kratos-789")
			}
			if finder.gotDisplayName != tt.want {
				t.Errorf("display name = %q, want %q", finder.gotDisplayName, tt.want)
			}
		})
	}
}

// OptionalAuth shares resolveUser with KratosAuth, so the trait must arrive on
// that path too — a visitor's first request can land on a public page.
func TestOptionalAuth_PassesNameTraitToFinder(t *testing.T) {
	validSession := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, kratosSessionJSONWithTraits("kratos-456", `{"name":"Grace Hopper"}`))
	})
	finder := &mockUserFinder{user: &domain.User{
		ID:               "user-1",
		KratosIdentityID: "kratos-456",
		Role:             domain.RoleMember,
		IsActive:         true,
	}}

	rec, gotUser := optionalAuthCase(t, validSession, finder, "ory_session=valid")

	assertStatus(t, rec, http.StatusOK)
	if gotUser == nil {
		t.Fatal("expected the signed-in user in context, got none")
	}
	if finder.gotDisplayName != "Grace Hopper" {
		t.Errorf("display name = %q, want %q", finder.gotDisplayName, "Grace Hopper")
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

// --- OptionalAuth tests ---

// optionalAuthCase runs one request through OptionalAuth and reports the status
// and whichever user reached the inner handler.
func optionalAuthCase(t *testing.T, kratosHandler http.HandlerFunc, finder *mockUserFinder, cookie string) (*httptest.ResponseRecorder, *domain.User) {
	t.Helper()

	kratosServer := httptest.NewServer(kratosHandler)
	t.Cleanup(kratosServer.Close)

	var gotUser *domain.User
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, ok := middleware.UserFromContext(r.Context()); ok {
			gotUser = u
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"posts":[]}`))
	})

	handler := middleware.OptionalAuth(newKratosClient(kratosServer.URL), finder, testLogger())(inner)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec, gotUser
}

// The whole point of the optional variant: a reader with no session still gets
// the page. Rejecting here would make the public town feed unreadable to the
// logged-out visitors it exists for.
func TestOptionalAuth_NoCookieServesAnonymously(t *testing.T) {
	rec, gotUser := optionalAuthCase(t, http.NotFoundHandler().ServeHTTP, &mockUserFinder{}, "")

	assertStatus(t, rec, http.StatusOK)
	if gotUser != nil {
		t.Errorf("user in context = %v, want none", gotUser)
	}
	if rec.Body.String() != `{"posts":[]}` {
		t.Errorf("body = %q, want the inner handler's response", rec.Body.String())
	}
}

// A garbage or expired cookie means "no user", never a rejection — this is the
// fail-open property, and it is the one a later change is most likely to undo.
func TestOptionalAuth_GarbageCookieStillServesTheFeed(t *testing.T) {
	rejectSession := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	rec, gotUser := optionalAuthCase(t, rejectSession, &mockUserFinder{}, "ory_session=not-a-real-session")

	assertStatus(t, rec, http.StatusOK)
	if gotUser != nil {
		t.Errorf("user in context = %v, want none", gotUser)
	}
	if rec.Body.String() != `{"posts":[]}` {
		t.Errorf("body = %q, want the inner handler's response", rec.Body.String())
	}
}

// A valid session with no matching local user is the same story: anonymous,
// not rejected.
func TestOptionalAuth_UnknownIdentityServesAnonymously(t *testing.T) {
	validSession := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, kratosSessionJSON("kratos-unknown"))
	})

	rec, gotUser := optionalAuthCase(t, validSession, &mockUserFinder{user: nil}, "ory_session=valid")

	assertStatus(t, rec, http.StatusOK)
	if gotUser != nil {
		t.Errorf("user in context = %v, want none", gotUser)
	}
}

// A failed lookup degrades to the anonymous view rather than 500ing the whole
// public page. The caller sees less, not nothing.
func TestOptionalAuth_LookupFailureServesAnonymously(t *testing.T) {
	validSession := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, kratosSessionJSON("kratos-456"))
	})

	rec, gotUser := optionalAuthCase(t, validSession, &mockUserFinder{err: fmt.Errorf("db down")}, "ory_session=valid")

	assertStatus(t, rec, http.StatusOK)
	if gotUser != nil {
		t.Errorf("user in context = %v, want none", gotUser)
	}
}

// And when the session is good, the user must actually arrive — otherwise the
// personalization this exists for never happens.
func TestOptionalAuth_ValidSessionPopulatesTheUser(t *testing.T) {
	validSession := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, kratosSessionJSON("kratos-456"))
	})
	user := &domain.User{
		ID:               "user-1",
		KratosIdentityID: "kratos-456",
		Role:             domain.RoleModerator,
		IsActive:         true,
	}

	rec, gotUser := optionalAuthCase(t, validSession, &mockUserFinder{user: user}, "ory_session=valid")

	assertStatus(t, rec, http.StatusOK)
	if gotUser == nil {
		t.Fatal("expected the signed-in user in context, got none")
	}
	if gotUser.ID != "user-1" || gotUser.Role != domain.RoleModerator {
		t.Errorf("user = %+v, want user-1 as moderator", gotUser)
	}
}

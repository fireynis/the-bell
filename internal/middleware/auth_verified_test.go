package middleware_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
)

// kratosSessionJSONWithAddresses is kratosSessionJSON with the identity's
// verifiable_addresses array supplied verbatim, so a test can hand the
// middleware the shapes a differently-configured Kratos actually produces —
// including no array at all.
func kratosSessionJSONWithAddresses(identityID, addresses string) string {
	identity := fmt.Sprintf(`{
		"id": %q,
		"schema_id": "default",
		"schema_url": "http://kratos/schemas/default",
		"traits": {"email":"ada@example.com"}`, identityID)
	if addresses != "" {
		identity += fmt.Sprintf(`,
		"verifiable_addresses": %s`, addresses)
	}
	return fmt.Sprintf(`{"id":"session-id","active":true,"identity":%s}}`, identity)
}

const verifiedAddress = `[{"id":"addr-1","value":"ada@example.com","verified":true,"via":"email","status":"completed"}]`
const unverifiedAddress = `[{"id":"addr-1","value":"ada@example.com","verified":false,"via":"email","status":"sent"}]`

// verifiedThroughAuth runs a request through KratosAuth followed by
// RequireVerifiedEmail — the order routes.go composes them in — against a
// Kratos returning the given identity addresses.
func verifiedThroughAuth(t *testing.T, addresses string) *httptest.ResponseRecorder {
	t.Helper()

	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, kratosSessionJSONWithAddresses("kratos-verify", addresses))
	}))
	t.Cleanup(kratosServer.Close)

	finder := &mockUserFinder{user: &domain.User{
		ID:               "user-1",
		KratosIdentityID: "kratos-verify",
		Role:             domain.RoleMember,
		IsActive:         true,
	}}

	handler := middleware.KratosAuth(newKratosClient(kratosServer.URL), finder, testLogger())(
		middleware.RequireVerifiedEmail(okHandler()),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	req.Header.Set("Cookie", "ory_session=valid")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// The enforcement path, end to end from the session: a resident who confirmed
// their address participates, and one who has not is told to go and confirm it.
func TestRequireVerifiedEmail_ThroughKratosAuth(t *testing.T) {
	tests := []struct {
		name       string
		addresses  string
		wantStatus int
		wantError  string
	}{
		{
			name:       "a verified address passes",
			addresses:  verifiedAddress,
			wantStatus: http.StatusOK,
		},
		{
			name:       "an unverified address is blocked",
			addresses:  unverifiedAddress,
			wantStatus: http.StatusForbidden,
			wantError:  "email not verified",
		},
		{
			// A schema that declares no verifiable addresses cannot prove
			// anything was verified, so it is not treated as if it had.
			name:       "no verifiable_addresses at all is unverified",
			addresses:  "",
			wantStatus: http.StatusForbidden,
			wantError:  "email not verified",
		},
		{
			name:       "an empty verifiable_addresses array is unverified",
			addresses:  `[]`,
			wantStatus: http.StatusForbidden,
			wantError:  "email not verified",
		},
		{
			// One confirmed address is enough. A resident who added a second
			// address and has not confirmed it is not thereby locked out.
			name: "one verified address among several is enough",
			addresses: `[{"id":"a","value":"old@example.com","verified":false,"via":"email","status":"sent"},
			             {"id":"b","value":"ada@example.com","verified":true,"via":"email","status":"completed"}]`,
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := verifiedThroughAuth(t, tt.addresses)

			assertStatus(t, rec, tt.wantStatus)
			if tt.wantError != "" {
				assertErrorBody(t, rec, tt.wantError)
			}
		})
	}
}

// The message has to be distinguishable from the other 403s. "forbidden" and
// "account suspended" tell a resident to find a moderator; this one tells them
// to open their inbox, and the client cannot tell those apart from the status
// code alone.
func TestRequireVerifiedEmail_MessageIsDistinct(t *testing.T) {
	rec := verifiedThroughAuth(t, unverifiedAddress)

	for _, other := range []string{"forbidden", "account suspended", "unauthorized"} {
		if body := rec.Body.String(); body == fmt.Sprintf(`{"error":%q}`, other) {
			t.Errorf("verification failure is indistinguishable from %q", other)
		}
	}
}

// Reached without the auth middleware there is no session to have asked about,
// which is a wiring fault rather than an unverified resident.
func TestRequireVerifiedEmail_NoUserInContext(t *testing.T) {
	rec := httptest.NewRecorder()
	middleware.RequireVerifiedEmail(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	assertStatus(t, rec, http.StatusUnauthorized)
	assertErrorBody(t, rec, "unauthorized")
}

// A user in context with no recorded verification state is unverified. "Nobody
// asked Kratos" is not evidence that the address was confirmed.
func TestRequireVerifiedEmail_UnrecordedStateIsUnverified(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(middleware.WithUser(req.Context(), &domain.User{
		ID: "u1", Role: domain.RoleMember, IsActive: true,
	}))
	rec := httptest.NewRecorder()

	middleware.RequireVerifiedEmail(okHandler()).ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusForbidden)
	assertErrorBody(t, rec, "email not verified")
}

func TestEmailVerified_ContextRoundTrip(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	// Absent is reported as absent, not as false-and-known.
	if _, ok := middleware.EmailVerifiedFromContext(req.Context()); ok {
		t.Error("an untouched context reports a recorded verification state")
	}

	for _, want := range []bool{true, false} {
		ctx := middleware.WithEmailVerified(req.Context(), want)
		got, ok := middleware.EmailVerifiedFromContext(ctx)
		if !ok {
			t.Fatalf("WithEmailVerified(%v) did not record anything", want)
		}
		if got != want {
			t.Errorf("EmailVerifiedFromContext() = %v, want %v", got, want)
		}
	}
}

// OptionalAuth shares resolveUser with KratosAuth, so the verification state
// must be recorded on that path too — otherwise a route that later composed the
// two would treat every signed-in reader as unverified.
func TestOptionalAuth_RecordsEmailVerification(t *testing.T) {
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, kratosSessionJSONWithAddresses("kratos-verify", verifiedAddress))
	}))
	defer kratosServer.Close()

	finder := &mockUserFinder{user: &domain.User{
		ID: "user-1", KratosIdentityID: "kratos-verify", Role: domain.RoleMember, IsActive: true,
	}}

	var verified, recorded bool
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		verified, recorded = middleware.EmailVerifiedFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.OptionalAuth(newKratosClient(kratosServer.URL), finder, testLogger())(inner)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil)
	req.Header.Set("Cookie", "ory_session=valid")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if !recorded {
		t.Fatal("OptionalAuth recorded no verification state for a valid session")
	}
	if !verified {
		t.Error("verification state = false for an identity with a verified address")
	}
}

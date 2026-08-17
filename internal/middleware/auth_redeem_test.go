package middleware_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
)

// recordingRedeemer captures the redemption attempts the middleware makes.
type recordingRedeemer struct {
	calls []redeemCall
	err   error
	// promote stands in for the real service raising the caller's copy of the
	// user once the vouch lands.
	promote bool
}

type redeemCall struct {
	userID string
	email  string
}

func (r *recordingRedeemer) Redeem(_ context.Context, user *domain.User, email string) error {
	r.calls = append(r.calls, redeemCall{userID: user.ID, email: email})
	if r.err != nil {
		return r.err
	}
	if r.promote {
		user.Role = domain.RoleMember
	}
	return nil
}

// roleReporter answers with the role the handler was given, which is what the
// SPA reads from GET /v1/me.
func roleReporter() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _ := middleware.UserFromContext(r.Context())
		fmt.Fprint(w, user.Role)
	})
}

func kratosSessionWithEmail(identityID, email string) string {
	return kratosSessionJSONWithTraits(identityID, fmt.Sprintf(`{"email":%q,"name":"New Resident"}`, email))
}

func serveWithRedeemer(t *testing.T, sessionJSON string, finder *mockUserFinder, redeemer *recordingRedeemer, next http.Handler) *httptest.ResponseRecorder {
	t.Helper()

	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, sessionJSON)
	}))
	t.Cleanup(kratosServer.Close)

	h := middleware.KratosAuth(newKratosClient(kratosServer.URL), finder, testLogger(),
		middleware.WithInviteRedeemer(redeemer))(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Cookie", "ory_session=valid")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestKratosAuth_RedeemsForAPendingUserUsingTheIdentitysAddress(t *testing.T) {
	finder := &mockUserFinder{
		user:    &domain.User{ID: "newcomer-1", Role: domain.RolePending, IsActive: true},
		created: true,
	}
	redeemer := &recordingRedeemer{promote: true}

	rec := serveWithRedeemer(t, kratosSessionWithEmail("kratos-123", "newcomer@example.com"),
		finder, redeemer, roleReporter())

	if len(redeemer.calls) != 1 {
		t.Fatalf("redeemed %d times, want 1", len(redeemer.calls))
	}
	if redeemer.calls[0].email != "newcomer@example.com" {
		t.Errorf("redeemed with %q, want the identity's address", redeemer.calls[0].email)
	}
	if redeemer.calls[0].userID != "newcomer-1" {
		t.Errorf("redeemed for %q", redeemer.calls[0].userID)
	}
	// The promotion the redemption caused has to be visible to this request's
	// handler, or the invitee's first page load renders the pending screen for
	// a membership they already have.
	if body := rec.Body.String(); body != string(domain.RoleMember) {
		t.Errorf("handler saw role %q, want %q", body, domain.RoleMember)
	}
}

// The address can live on the verifiable addresses rather than in a trait,
// depending on the town's identity schema. Either one is the identity's
// address, and redemption must work under both.
func TestKratosAuth_FallsBackToTheVerifiableAddress(t *testing.T) {
	session := fmt.Sprintf(`{
		"id": "session-id",
		"active": true,
		"identity": {
			"id": "kratos-123",
			"schema_id": "default",
			"schema_url": "http://kratos/schemas/default",
			"traits": {"name": "New Resident"},
			"verifiable_addresses": [{"id":"addr-1","value":%q,"verified":false,"via":"email","status":"pending"}]
		}
	}`, "newcomer@example.com")

	finder := &mockUserFinder{user: &domain.User{ID: "newcomer-1", Role: domain.RolePending, IsActive: true}}
	redeemer := &recordingRedeemer{}

	rec := serveWithRedeemer(t, session, finder, redeemer, okHandler())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want the session to resolve", rec.Code, rec.Body)
	}

	if len(redeemer.calls) != 1 || redeemer.calls[0].email != "newcomer@example.com" {
		t.Errorf("redeem calls = %+v, want one with the verifiable address", redeemer.calls)
	}
}

// Redemption is for people waiting to get in. Running it for everybody would
// put an invitation lookup in front of every request the town makes.
func TestKratosAuth_DoesNotRedeemForSettledRoles(t *testing.T) {
	for _, role := range []domain.Role{domain.RoleMember, domain.RoleModerator, domain.RoleCouncil, domain.RoleBanned} {
		t.Run(string(role), func(t *testing.T) {
			finder := &mockUserFinder{user: &domain.User{ID: "u", Role: role, IsActive: true}}
			redeemer := &recordingRedeemer{}

			serveWithRedeemer(t, kratosSessionWithEmail("kratos-123", "someone@example.com"),
				finder, redeemer, okHandler())

			if len(redeemer.calls) != 0 {
				t.Errorf("redeemed %d times for a %s, want none", len(redeemer.calls), role)
			}
		})
	}
}

// A resident's ability to sign in must never depend on the invitation
// machinery. The worst a failure here may cost is that somebody stays pending.
func TestKratosAuth_ARedemptionFailureDoesNotFailAuthentication(t *testing.T) {
	finder := &mockUserFinder{user: &domain.User{ID: "newcomer-1", Role: domain.RolePending, IsActive: true}}
	redeemer := &recordingRedeemer{err: errors.New("the trust graph is unreachable")}

	rec := serveWithRedeemer(t, kratosSessionWithEmail("kratos-123", "newcomer@example.com"),
		finder, redeemer, okHandler())

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d: a failed redemption must not block the request", rec.Code, http.StatusOK)
	}
}

// An identity with no address at all is not an authentication failure — it just
// has no invitation to match. The service treats the empty address as "nothing
// waiting", and the middleware must still let them in.
func TestKratosAuth_AnIdentityWithNoAddressStillAuthenticates(t *testing.T) {
	finder := &mockUserFinder{user: &domain.User{ID: "newcomer-1", Role: domain.RolePending, IsActive: true}}
	redeemer := &recordingRedeemer{}

	rec := serveWithRedeemer(t, kratosSessionJSONWithTraits("kratos-123", `{"name":"Nameless"}`),
		finder, redeemer, okHandler())

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if len(redeemer.calls) != 1 || redeemer.calls[0].email != "" {
		t.Errorf("redeem calls = %+v, want one with an empty address", redeemer.calls)
	}
}

// Without the option nothing redeems, which is every deployment that predates
// invitations and any town that admits people some other way.
func TestKratosAuth_WithoutARedeemerNothingHappens(t *testing.T) {
	kratosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, kratosSessionWithEmail("kratos-123", "newcomer@example.com"))
	}))
	defer kratosServer.Close()

	finder := &mockUserFinder{user: &domain.User{ID: "newcomer-1", Role: domain.RolePending, IsActive: true}}
	h := middleware.KratosAuth(newKratosClient(kratosServer.URL), finder, testLogger())(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Cookie", "ory_session=valid")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

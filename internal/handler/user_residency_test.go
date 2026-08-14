package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/handler"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/service"
)

func residencyRequest(t *testing.T, users *stubProfileService, caller *domain.User, body string) *httptest.ResponseRecorder {
	t.Helper()

	h := handler.NewUserHandler(users, nil, nil)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/me/residency-claim", strings.NewReader(body))
	if caller != nil {
		req = req.WithContext(middleware.WithUser(req.Context(), caller))
	}
	rec := httptest.NewRecorder()
	h.UpdateResidencyClaim(rec, req)
	return rec
}

var pendingCaller = &domain.User{ID: "resident-1", Role: domain.RolePending, IsActive: true}

// 204 with no body is the whole contract: there is nothing to read back that
// the client did not just send, and the claim must not appear on the wire a
// second time.
func TestUserHandler_UpdateResidencyClaim_AnswersNoContent(t *testing.T) {
	users := &stubProfileService{}

	rec := residencyRequest(t, users, pendingCaller, `{"claim":"12 Mill Lane"}`)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want it empty", rec.Body.String())
	}
	if !users.claimSet {
		t.Fatal("the handler never called the service")
	}
	if users.gotID != "resident-1" {
		t.Errorf("wrote the claim for %q, want the authenticated caller", users.gotID)
	}
	if users.gotClaim != "12 Mill Lane" {
		t.Errorf("claim = %q, want it passed through untouched", users.gotClaim)
	}
}

// The claim is written for whoever is signed in, never for an id in the body.
// A pending resident is exactly who uses this endpoint, so no role floor
// applies — but the identity is the session's, not the caller's to choose.
func TestUserHandler_UpdateResidencyClaim_WritesForTheAuthenticatedCaller(t *testing.T) {
	users := &stubProfileService{}

	// An id in the body is an unknown field, which Decode refuses outright —
	// the strongest form of "you cannot write somebody else's claim".
	rec := residencyRequest(t, users, pendingCaller, `{"claim":"12 Mill Lane","user_id":"someone-else"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if users.claimSet {
		t.Error("the handler called the service with an unknown field in the body")
	}
}

// Clearing is a normal request, not an error, and must still reach the service
// — otherwise "withdraw what I said" would silently do nothing.
func TestUserHandler_UpdateResidencyClaim_EmptyClaimReachesTheService(t *testing.T) {
	users := &stubProfileService{}

	rec := residencyRequest(t, users, pendingCaller, `{"claim":""}`)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if !users.claimSet {
		t.Fatal("clearing the claim never reached the service")
	}
	if users.gotClaim != "" {
		t.Errorf("claim = %q, want the empty string", users.gotClaim)
	}
}

func TestUserHandler_UpdateResidencyClaim_UnauthenticatedIs401(t *testing.T) {
	users := &stubProfileService{}

	rec := residencyRequest(t, users, nil, `{"claim":"12 Mill Lane"}`)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if users.claimSet {
		t.Error("an unauthenticated request reached the service")
	}
}

func TestUserHandler_UpdateResidencyClaim_MalformedBodyIs400(t *testing.T) {
	for _, body := range []string{``, `not json`, `{"claim":12}`} {
		users := &stubProfileService{}

		rec := residencyRequest(t, users, pendingCaller, body)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %q: status = %d, want 400", body, rec.Code)
		}
		if users.claimSet {
			t.Errorf("body %q reached the service", body)
		}
	}
}

// A claim the service refuses as too long comes back as a 400 carrying the
// reason, so the resident can shorten it rather than guessing.
func TestUserHandler_UpdateResidencyClaim_ServiceValidationBecomes400(t *testing.T) {
	users := &stubProfileService{claimErr: service.ErrValidation}

	rec := residencyRequest(t, users, pendingCaller, `{"claim":"far too long"}`)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/handler"
	"github.com/fireynis/the-bell/internal/middleware"
)

// A residency claim is the most sensitive thing a user record carries: it is
// where somebody lives, written in their own words, and it has exactly two
// readers — the resident who wrote it, and the council deciding whether to
// approve them.
//
// These tests are the pair of assertions that keeps it to those two.
// domain.User tags the field `json:"-"` so it cannot ride along wherever the
// struct is serialized, and two response types opt in by naming it: the
// approval queue's, and the self view's. Everything else must stay clean. The
// directory has the same shape of test for the same reason; this one is
// stricter, because a leaked trust score is embarrassing and a leaked home
// address is dangerous.

const secretClaim = "12 Mill Lane, behind the churchyard"

func residentWithClaim(id string, role domain.Role) *domain.User {
	return &domain.User{
		ID:             id,
		DisplayName:    "Ada",
		Role:           role,
		IsActive:       true,
		TrustScore:     50,
		JoinedAt:       time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		ResidencyClaim: secretClaim,
	}
}

// assertNoClaim fails if the claim, or the key naming it, reached the wire.
func assertNoClaim(t *testing.T, endpoint string, body string) {
	t.Helper()
	if strings.Contains(body, secretClaim) {
		t.Errorf("%s published a resident's home address: %s", endpoint, body)
	}
	if strings.Contains(body, "residency_claim") {
		t.Errorf("%s published the residency_claim key: %s", endpoint, body)
	}
}

// The first of the two endpoints that may see it.
func TestApprovalQueue_CarriesTheResidencyClaim(t *testing.T) {
	svc := &stubApprovalService{pending: []*domain.User{residentWithClaim("resident-1", domain.RolePending)}}
	h := handler.NewApprovalHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vouches/pending", nil)
	req = req.WithContext(middleware.WithUser(req.Context(), councilCaller))
	rec := httptest.NewRecorder()
	h.ListPending(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Users []struct {
			ID             string `json:"id"`
			DisplayName    string `json:"display_name"`
			ResidencyClaim string `json:"residency_claim"`
		} `json:"users"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v; body: %s", err, rec.Body.String())
	}
	if len(body.Users) != 1 {
		t.Fatalf("%d pending users, want 1", len(body.Users))
	}
	if body.Users[0].ResidencyClaim != secretClaim {
		t.Errorf("residency_claim = %q, want the claim the resident made", body.Users[0].ResidencyClaim)
	}
	// The rest of the pending shape is unchanged: the council's screen has
	// read the whole user record since the town's first release.
	if body.Users[0].ID != "resident-1" || body.Users[0].DisplayName != "Ada" {
		t.Errorf("the queue lost fields it used to carry: %+v", body.Users[0])
	}
}

// A resident who has said nothing gets the key with an empty string, so the
// council's screen can tell "said nothing" from "not asked" without a second
// rule.
func TestApprovalQueue_EmptyClaimIsStillPresent(t *testing.T) {
	resident := residentWithClaim("resident-1", domain.RolePending)
	resident.ResidencyClaim = ""
	svc := &stubApprovalService{pending: []*domain.User{resident}}
	h := handler.NewApprovalHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vouches/pending", nil)
	req = req.WithContext(middleware.WithUser(req.Context(), councilCaller))
	rec := httptest.NewRecorder()
	h.ListPending(rec, req)

	if !strings.Contains(rec.Body.String(), `"residency_claim":""`) {
		t.Errorf("body = %s, want an explicit empty residency_claim", rec.Body.String())
	}
}

// ...and every endpoint that shows a resident to somebody else must not.

func TestPublicProfile_DoesNotPublishTheResidencyClaim(t *testing.T) {
	users := &stubProfileService{user: residentWithClaim("resident-1", domain.RoleMember)}
	h := handler.NewUserHandler(users, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/resident-1", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "resident-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.GetByID(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	assertNoClaim(t, "GET /users/{id}", rec.Body.String())
}

func TestDirectory_DoesNotPublishTheResidencyClaim(t *testing.T) {
	users := &stubProfileService{
		directory: []*domain.User{residentWithClaim("resident-1", domain.RolePending)},
		total:     1,
	}

	rec := directoryRequest(t, users, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	assertNoClaim(t, "GET /users", rec.Body.String())
}

// The other endpoint that may see it: the resident's own profile. They wrote
// the claim, so there is no disclosure in showing it back to them — and the
// field that collects it has to prefill, or changing one word means retyping an
// address from memory in a box that looks like it lost the answer.
//
// GET /v1/me and GET /v1/users/me are the same handler, so this covers both.
func TestOwnProfile_CarriesTheResidencyClaim(t *testing.T) {
	caller := residentWithClaim("resident-1", domain.RoleMember)
	users := &stubProfileService{user: caller}
	h := handler.NewUserHandler(users, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	req = req.WithContext(middleware.WithUser(req.Context(), caller))
	rec := httptest.NewRecorder()
	h.GetMe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		ResidencyClaim string `json:"residency_claim"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v; body: %s", err, rec.Body.String())
	}
	if body.ResidencyClaim != secretClaim {
		t.Errorf("residency_claim = %q, want %q", body.ResidencyClaim, secretClaim)
	}
}

// The updated profile is a self view too, so it carries the claim for the same
// reason — and specifically so that saving a display name does not hand the
// client back a response whose residency field looks empty.
func TestUpdatedOwnProfile_CarriesTheResidencyClaim(t *testing.T) {
	caller := residentWithClaim("resident-1", domain.RoleMember)
	users := &stubProfileService{user: caller}
	h := handler.NewUserHandler(users, nil, nil)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/me",
		strings.NewReader(`{"display_name":"Ada","bio":"","avatar_url":""}`))
	req = req.WithContext(middleware.WithUser(req.Context(), caller))
	rec := httptest.NewRecorder()
	h.UpdateMe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), secretClaim) {
		t.Errorf("the updated profile dropped the residency claim: %s", rec.Body.String())
	}
}

// A resident who has said nothing gets the key with an empty string, so the
// client prefilling the box does not have to treat "absent" and "blank" as two
// cases.
func TestOwnProfile_EmptyResidencyClaimIsStillPresent(t *testing.T) {
	caller := residentWithClaim("resident-1", domain.RoleMember)
	caller.ResidencyClaim = ""
	users := &stubProfileService{user: caller}
	h := handler.NewUserHandler(users, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	req = req.WithContext(middleware.WithUser(req.Context(), caller))
	rec := httptest.NewRecorder()
	h.GetMe(rec, req)

	if !strings.Contains(rec.Body.String(), `"residency_claim":""`) {
		t.Errorf("body = %s, want an explicit empty residency_claim", rec.Body.String())
	}
}

// The self view is the ONLY profile shape that carries it. ownProfileResponse
// holds the field rather than the userProfileResponse it embeds, so a stranger
// reading the same person's profile cannot pick it up from the shared type —
// this is the test that would fail if somebody moved the field up.
func TestPublicProfileOfAResidentWithAClaim_StaysClean(t *testing.T) {
	subject := residentWithClaim("resident-1", domain.RoleMember)
	stranger := &domain.User{ID: "stranger", Role: domain.RoleMember, IsActive: true}
	users := &stubProfileService{user: subject}
	h := handler.NewUserHandler(users, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/resident-1", nil)
	req = req.WithContext(middleware.WithUser(req.Context(), stranger))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "resident-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.GetByID(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	assertNoClaim(t, "GET /users/{id} read by somebody else", rec.Body.String())
}

// Approving somebody ends the review, and with it the reason the council could
// see the claim. The approved user comes back without it.
func TestApprove_DoesNotReturnTheResidencyClaim(t *testing.T) {
	approved := residentWithClaim("resident-1", domain.RoleMember)
	svc := &stubApprovalService{approved: approved}
	h := handler.NewApprovalHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/vouches/approve/resident-1", nil)
	req = req.WithContext(middleware.WithUser(req.Context(), councilCaller))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "resident-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.Approve(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	assertNoClaim(t, "POST /vouches/approve/{id}", rec.Body.String())
}

type stubApprovalService struct {
	pending  []*domain.User
	approved *domain.User
}

func (s *stubApprovalService) ListPending(context.Context) ([]*domain.User, error) {
	return s.pending, nil
}

func (s *stubApprovalService) Approve(context.Context, string) (*domain.User, error) {
	return s.approved, nil
}

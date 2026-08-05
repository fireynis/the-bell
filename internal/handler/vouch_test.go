package handler_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/handler"
	"github.com/fireynis/the-bell/internal/service"
)

// fakeVouchService stands in for VouchService. The trust rules themselves are
// the service's, and are tested there; what these tests pin down is that the
// handler passes the right actor through and turns each rule's error into the
// right status.
type fakeVouchService struct {
	vouch     *domain.Vouch
	vouchErr  error
	revokeErr error

	gotVoucherID string
	gotVoucheeID string
	gotVouchID   string
	gotActorID   string
	vouchCalls   int
	revokeCalls  int
}

func (f *fakeVouchService) Vouch(_ context.Context, voucherID, voucheeID string) (*domain.Vouch, error) {
	f.vouchCalls++
	f.gotVoucherID, f.gotVoucheeID = voucherID, voucheeID
	if f.vouchErr != nil {
		return nil, f.vouchErr
	}
	return f.vouch, nil
}

func (f *fakeVouchService) Revoke(_ context.Context, vouchID, actorID string) error {
	f.revokeCalls++
	f.gotVouchID, f.gotActorID = vouchID, actorID
	return f.revokeErr
}

func testVoucher() *domain.User {
	return &domain.User{
		ID:         "voucher-1",
		Role:       domain.RoleMember,
		IsActive:   true,
		TrustScore: 75.0,
	}
}

func newVouchRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vouches", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// revokeRequest wires the {id} URL parameter the way chi would, since the
// handler reads it with chi.URLParam rather than from the path directly.
func revokeRequest(id string) *http.Request {
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/vouches/"+id, nil)
	return withChiURLParam(req, "id", id)
}

func TestVouchHandler_Create_VouchesAsTheAuthenticatedUser(t *testing.T) {
	svc := &fakeVouchService{vouch: &domain.Vouch{
		ID:        "vouch-1",
		VoucherID: "voucher-1",
		VoucheeID: "vouchee-1",
		Status:    domain.VouchActive,
	}}
	h := handler.NewVouchHandler(svc)

	rec := httptest.NewRecorder()
	h.Create(rec, withUser(newVouchRequest(`{"vouchee_id":"vouchee-1"}`), testVoucher()))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusCreated, rec.Body.String())
	}

	// The voucher is the session user, never anything the request body says —
	// otherwise a caller could spend someone else's trust.
	if svc.gotVoucherID != "voucher-1" {
		t.Errorf("voucher = %q, want the authenticated user %q", svc.gotVoucherID, "voucher-1")
	}
	if svc.gotVoucheeID != "vouchee-1" {
		t.Errorf("vouchee = %q, want %q", svc.gotVoucheeID, "vouchee-1")
	}

	var got domain.Vouch
	decodeBody(t, rec, &got)
	if got.ID != "vouch-1" {
		t.Errorf("returned vouch ID = %q, want %q", got.ID, "vouch-1")
	}
}

func TestVouchHandler_Create_RejectsAnonymousCallers(t *testing.T) {
	svc := &fakeVouchService{}
	h := handler.NewVouchHandler(svc)

	rec := httptest.NewRecorder()
	h.Create(rec, newVouchRequest(`{"vouchee_id":"vouchee-1"}`))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if svc.vouchCalls != 0 {
		t.Error("service was called for an unauthenticated request")
	}
}

func TestVouchHandler_Create_RejectsMalformedBodyWithoutCallingTheService(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"not json", `{`},
		{"unknown field", `{"vouchee_id":"v-1","trust_score":100}`},
		{"missing vouchee_id", `{}`},
		{"blank vouchee_id", `{"vouchee_id":"   "}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeVouchService{}
			h := handler.NewVouchHandler(svc)

			rec := httptest.NewRecorder()
			h.Create(rec, withUser(newVouchRequest(tt.body), testVoucher()))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if svc.vouchCalls != 0 {
				t.Error("service was called with a malformed request")
			}
		})
	}
}

// Every one of these is an ordinary thing a resident can do by accident. None
// of them is a server fault, so none may surface as a 500.
func TestVouchHandler_Create_MapsTrustRuleRefusalsToClientErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"vouching for yourself", fmt.Errorf("%w: cannot vouch for yourself", service.ErrValidation), http.StatusBadRequest},
		{"vouch already exists for the pair", fmt.Errorf("%w: vouch already exists for this pair", service.ErrValidation), http.StatusBadRequest},
		{"daily vouch limit reached", fmt.Errorf("%w: daily vouch limit (3) reached", service.ErrValidation), http.StatusBadRequest},
		{"vouch would create a cycle", fmt.Errorf("%w: vouch would create a cycle in the trust graph", service.ErrValidation), http.StatusBadRequest},
		{"voucher lacks the trust to vouch", fmt.Errorf("%w: voucher does not meet trust requirements", service.ErrForbidden), http.StatusForbidden},
		{"vouchee does not exist", fmt.Errorf("looking up vouchee: %w", service.ErrNotFound), http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := handler.NewVouchHandler(&fakeVouchService{vouchErr: tt.err})

			rec := httptest.NewRecorder()
			h.Create(rec, withUser(newVouchRequest(`{"vouchee_id":"vouchee-1"}`), testVoucher()))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

// An unreachable graph or database is a server fault and must not be reported
// as though the caller did something wrong.
func TestVouchHandler_Create_InfrastructureFailureIsA500(t *testing.T) {
	h := handler.NewVouchHandler(&fakeVouchService{vouchErr: errors.New("checking cycle: AGE unavailable")})

	rec := httptest.NewRecorder()
	h.Create(rec, withUser(newVouchRequest(`{"vouchee_id":"vouchee-1"}`), testVoucher()))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if strings.Contains(rec.Body.String(), "AGE") {
		t.Errorf("response leaked infrastructure detail: %s", rec.Body.String())
	}
}

func TestVouchHandler_Revoke_RevokesAsTheAuthenticatedUser(t *testing.T) {
	svc := &fakeVouchService{}
	h := handler.NewVouchHandler(svc)

	rec := httptest.NewRecorder()
	h.Revoke(rec, withUser(revokeRequest("vouch-1"), testVoucher()))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if svc.gotVouchID != "vouch-1" {
		t.Errorf("vouch id = %q, want %q", svc.gotVouchID, "vouch-1")
	}
	// The service decides whether this actor may revoke; the handler's job is
	// only to report who is asking.
	if svc.gotActorID != "voucher-1" {
		t.Errorf("actor = %q, want the authenticated user %q", svc.gotActorID, "voucher-1")
	}
	if rec.Body.Len() != 0 {
		t.Errorf("204 response carried a body: %s", rec.Body.String())
	}
}

func TestVouchHandler_Revoke_RejectsAnonymousCallers(t *testing.T) {
	svc := &fakeVouchService{}
	h := handler.NewVouchHandler(svc)

	rec := httptest.NewRecorder()
	h.Revoke(rec, revokeRequest("vouch-1"))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if svc.revokeCalls != 0 {
		t.Error("service was called for an unauthenticated request")
	}
}

func TestVouchHandler_Revoke_MapsServiceRefusalsToClientErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"not the voucher and not a moderator", service.ErrForbidden, http.StatusForbidden},
		{"vouch does not exist", fmt.Errorf("looking up vouch: %w", service.ErrNotFound), http.StatusNotFound},
		{"vouch is already revoked", fmt.Errorf("%w: vouch is already revoked", service.ErrValidation), http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := handler.NewVouchHandler(&fakeVouchService{revokeErr: tt.err})

			rec := httptest.NewRecorder()
			h.Revoke(rec, withUser(revokeRequest("vouch-1"), testVoucher()))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestVouchHandler_Revoke_RejectsBlankID(t *testing.T) {
	svc := &fakeVouchService{}
	h := handler.NewVouchHandler(svc)

	rec := httptest.NewRecorder()
	h.Revoke(rec, withUser(revokeRequest(""), testVoucher()))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if svc.revokeCalls != 0 {
		t.Error("service was called with a blank vouch id")
	}
}

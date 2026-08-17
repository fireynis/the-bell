package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/handler"
	"github.com/fireynis/the-bell/internal/service"
)

// fakeInviteService stands in for InviteService. The rules are the service's
// and are tested there; what these tests pin down is the wire shape — what
// reaches the client and, more importantly, what does not.
type fakeInviteService struct {
	creation  *service.InviteCreation
	createErr error
	invites   []*domain.Invite
	listErr   error
	revokeErr error
	lookup    *service.InviteLookup
	lookupErr error

	gotInviter  *domain.User
	gotEmail    string
	gotNote     string
	gotListID   string
	gotRevokeID string
	gotActorID  string
	gotToken    string
}

func (f *fakeInviteService) Create(_ context.Context, inviter *domain.User, email, note string) (*service.InviteCreation, error) {
	f.gotInviter, f.gotEmail, f.gotNote = inviter, email, note
	if f.createErr != nil {
		return nil, f.createErr
	}
	return f.creation, nil
}

func (f *fakeInviteService) List(_ context.Context, inviterID string) ([]*domain.Invite, error) {
	f.gotListID = inviterID
	return f.invites, f.listErr
}

func (f *fakeInviteService) Revoke(_ context.Context, id, inviterID string) error {
	f.gotRevokeID, f.gotActorID = id, inviterID
	return f.revokeErr
}

func (f *fakeInviteService) Lookup(_ context.Context, rawToken string) (*service.InviteLookup, error) {
	f.gotToken = rawToken
	if f.lookupErr != nil {
		return nil, f.lookupErr
	}
	return f.lookup, nil
}

func testInviter() *domain.User {
	return &domain.User{ID: "inviter-1", DisplayName: "Ana", Role: domain.RoleMember, IsActive: true, TrustScore: 75}
}

// The handler derives status against the wall clock, so these fixtures are
// relative to it rather than to a fixed date that would drift into the past.
var inviteNow = time.Now()

func openInvite(id, email string) *domain.Invite {
	return &domain.Invite{
		ID: id, Email: email, Note: "hello", InviterID: "inviter-1",
		CreatedAt: inviteNow, ExpiresAt: inviteNow.Add(30 * 24 * time.Hour),
	}
}

func TestInviteHandler_Create_ReturnsTheInvitationAndItsLink(t *testing.T) {
	invite := openInvite("invite-1", "newcomer@example.com")
	svc := &fakeInviteService{creation: &service.InviteCreation{
		Invite:    invite,
		URL:       "https://bell.example.test/auth/registration?invite=raw-token",
		EmailSent: true,
	}}
	h := handler.NewInviteHandler(svc)

	req := withUser(httptest.NewRequest(http.MethodPost, "/api/v1/invites",
		strings.NewReader(`{"email":"newcomer@example.com","note":"hello"}`)), testInviter())
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusCreated, rec.Body)
	}
	var got struct {
		Invite struct {
			ID        string `json:"id"`
			Email     string `json:"email"`
			Note      string `json:"note"`
			Status    string `json:"status"`
			ExpiresAt string `json:"expires_at"`
		} `json:"invite"`
		InviteURL  string `json:"invite_url"`
		EmailSent  bool   `json:"email_sent"`
		EmailError string `json:"email_error"`
	}
	decodeBody(t, rec, &got)

	if got.Invite.ID != "invite-1" || got.Invite.Email != "newcomer@example.com" {
		t.Errorf("invite = %+v", got.Invite)
	}
	if got.Invite.Status != "open" {
		t.Errorf("status = %q, want open", got.Invite.Status)
	}
	if got.InviteURL != "https://bell.example.test/auth/registration?invite=raw-token" {
		t.Errorf("invite_url = %q", got.InviteURL)
	}
	if !got.EmailSent {
		t.Error("email_sent = false")
	}
	if got.EmailError != "" {
		t.Errorf("email_error = %q, want it absent on a successful send", got.EmailError)
	}
	if svc.gotInviter == nil || svc.gotInviter.ID != "inviter-1" {
		t.Errorf("service was called with inviter %+v, want the authenticated user", svc.gotInviter)
	}
}

func TestInviteHandler_Create_ReportsAFailedSendWithoutFailingTheRequest(t *testing.T) {
	svc := &fakeInviteService{creation: &service.InviteCreation{
		Invite:     openInvite("invite-1", "newcomer@example.com"),
		URL:        "/auth/registration?invite=raw-token",
		EmailSent:  false,
		EmailError: "the invitation could not be emailed; send the link yourself",
	}}
	h := handler.NewInviteHandler(svc)

	req := withUser(httptest.NewRequest(http.MethodPost, "/api/v1/invites",
		strings.NewReader(`{"email":"newcomer@example.com"}`)), testInviter())
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	// 201, because the invitation exists and works. The member needs the link
	// and the reason, not an error page.
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	var got map[string]any
	decodeBody(t, rec, &got)
	if got["email_sent"] != false {
		t.Errorf("email_sent = %v, want false", got["email_sent"])
	}
	if got["email_error"] == nil || got["email_error"] == "" {
		t.Error("email_error is missing on a failed send")
	}
}

func TestInviteHandler_Create_MapsServiceErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{"below the vouching threshold", service.ErrForbidden, http.StatusForbidden},
		{"budget spent or address taken", service.ErrValidation, http.StatusBadRequest},
		{"anything else", errors.New("database on fire"), http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := handler.NewInviteHandler(&fakeInviteService{createErr: tt.err})
			req := withUser(httptest.NewRequest(http.MethodPost, "/api/v1/invites",
				strings.NewReader(`{"email":"a@example.com"}`)), testInviter())
			rec := httptest.NewRecorder()

			h.Create(rec, req)

			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}
		})
	}
}

func TestInviteHandler_Create_RejectsAMalformedBody(t *testing.T) {
	svc := &fakeInviteService{}
	h := handler.NewInviteHandler(svc)

	req := withUser(httptest.NewRequest(http.MethodPost, "/api/v1/invites",
		strings.NewReader(`{"email":`)), testInviter())
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestInviteHandler_Create_RequiresAUser(t *testing.T) {
	h := handler.NewInviteHandler(&fakeInviteService{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/invites", strings.NewReader(`{"email":"a@example.com"}`))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// The token exists in exactly one response, the one that created the
// invitation. Everything that reads invitations back must be unable to leak it,
// and the DTO is what guarantees that — there is no field for it to travel in.
func TestInviteHandler_List_CarriesNoTokenAndNoIDsButTheInvitationsOwn(t *testing.T) {
	consumedAt := inviteNow.Add(-time.Hour)
	accepted := openInvite("invite-2", "joined@example.com")
	accepted.ConsumedAt = &consumedAt
	accepted.ConsumedBy = "newcomer-1"
	accepted.ConsumedByDisplayName = "Dana"

	svc := &fakeInviteService{invites: []*domain.Invite{openInvite("invite-1", "waiting@example.com"), accepted}}
	h := handler.NewInviteHandler(svc)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/v1/invites", nil), testInviter())
	rec := httptest.NewRecorder()

	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, forbidden := range []string{"token", "inviter_id", "consumed_by\"", "newcomer-1"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("response contains %q:\n%s", forbidden, body)
		}
	}

	var got struct {
		Invites []struct {
			ID                    string `json:"id"`
			Email                 string `json:"email"`
			Status                string `json:"status"`
			ConsumedByDisplayName string `json:"consumed_by_display_name"`
		} `json:"invites"`
	}
	decodeBody(t, rec, &got)
	if len(got.Invites) != 2 {
		t.Fatalf("returned %d invitations, want 2", len(got.Invites))
	}
	if svc.gotListID != "inviter-1" {
		t.Errorf("listed invitations for %q, want the authenticated user", svc.gotListID)
	}

	byID := map[string]string{}
	names := map[string]string{}
	for _, invite := range got.Invites {
		byID[invite.ID] = invite.Status
		names[invite.ID] = invite.ConsumedByDisplayName
	}
	if byID["invite-1"] != "open" {
		t.Errorf("invite-1 status = %q, want open", byID["invite-1"])
	}
	if byID["invite-2"] != "accepted" {
		t.Errorf("invite-2 status = %q, want accepted", byID["invite-2"])
	}
	if names["invite-2"] != "Dana" {
		t.Errorf("accepted invitation names %q, want Dana", names["invite-2"])
	}
	if names["invite-1"] != "" {
		t.Errorf("an unaccepted invitation names %q, want nobody", names["invite-1"])
	}
}

func TestInviteHandler_List_DerivesExpiryAtReadTime(t *testing.T) {
	expired := openInvite("invite-1", "waiting@example.com")
	expired.ExpiresAt = time.Now().Add(-time.Hour)
	h := handler.NewInviteHandler(&fakeInviteService{invites: []*domain.Invite{expired}})

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/v1/invites", nil), testInviter())
	rec := httptest.NewRecorder()

	h.List(rec, req)

	var got struct {
		Invites []struct {
			Status string `json:"status"`
		} `json:"invites"`
	}
	decodeBody(t, rec, &got)
	if len(got.Invites) != 1 || got.Invites[0].Status != "expired" {
		t.Errorf("status = %+v, want expired without anything having swept the table", got.Invites)
	}
}

func TestInviteHandler_List_ReturnsAnEmptyArrayNotNull(t *testing.T) {
	h := handler.NewInviteHandler(&fakeInviteService{})

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/v1/invites", nil), testInviter())
	rec := httptest.NewRecorder()

	h.List(rec, req)

	var raw map[string]json.RawMessage
	decodeBody(t, rec, &raw)
	if string(raw["invites"]) != "[]" {
		t.Errorf("invites = %s, want []", raw["invites"])
	}
}

func TestInviteHandler_Revoke_PassesTheCallerAsTheOwner(t *testing.T) {
	svc := &fakeInviteService{}
	h := handler.NewInviteHandler(svc)

	req := withUser(withChiURLParam(
		httptest.NewRequest(http.MethodDelete, "/api/v1/invites/invite-1", nil), "id", "invite-1"), testInviter())
	rec := httptest.NewRecorder()

	h.Revoke(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if svc.gotRevokeID != "invite-1" || svc.gotActorID != "inviter-1" {
		t.Errorf("revoked %q as %q, want invite-1 as inviter-1", svc.gotRevokeID, svc.gotActorID)
	}
}

// Somebody else's invitation must not be distinguishable from one that does not
// exist, so the service's ErrNotFound has to arrive as a plain 404.
func TestInviteHandler_Revoke_IsA404ForAnythingNotTheCallersOwnOpenInvitation(t *testing.T) {
	h := handler.NewInviteHandler(&fakeInviteService{revokeErr: service.ErrNotFound})

	req := withUser(withChiURLParam(
		httptest.NewRequest(http.MethodDelete, "/api/v1/invites/somebody-elses", nil), "id", "somebody-elses"), testInviter())
	rec := httptest.NewRecorder()

	h.Revoke(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if strings.Contains(rec.Body.String(), "forbidden") {
		t.Errorf("body = %s, want no hint that the invitation belongs to somebody", rec.Body)
	}
}

func TestInviteHandler_Lookup_GreetsTheInvitee(t *testing.T) {
	svc := &fakeInviteService{lookup: &service.InviteLookup{
		Email: "newcomer@example.com", TownName: "Bellville", InviterDisplayName: "Ana",
	}}
	h := handler.NewInviteHandler(svc)

	rec := httptest.NewRecorder()
	h.Lookup(rec, httptest.NewRequest(http.MethodGet, "/api/v1/invites/lookup?token=raw-token", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got struct {
		Email              string `json:"email"`
		TownName           string `json:"town_name"`
		InviterDisplayName string `json:"inviter_display_name"`
		Status             string `json:"status"`
	}
	decodeBody(t, rec, &got)
	if got.Email != "newcomer@example.com" || got.TownName != "Bellville" || got.InviterDisplayName != "Ana" {
		t.Errorf("lookup = %+v", got)
	}
	if got.Status != "open" {
		t.Errorf("status = %q, want open", got.Status)
	}
	if svc.gotToken != "raw-token" {
		t.Errorf("service was asked for %q, want the query's token", svc.gotToken)
	}
}

// The uniformity is the security property. An unauthenticated caller working
// through guesses must get byte-identical answers whether the token never
// existed, was used, was withdrawn or has run out — and a service failure must
// not be distinguishable from those either, since a 500 on one token and a 404
// on another is itself a signal.
func TestInviteHandler_Lookup_AnswersEveryFailureIdentically(t *testing.T) {
	tests := []struct {
		name string
		err  error
		url  string
	}{
		{"unknown, consumed, revoked or expired", service.ErrNotFound, "/api/v1/invites/lookup?token=whatever"},
		{"no token at all", service.ErrNotFound, "/api/v1/invites/lookup"},
		{"empty token", service.ErrNotFound, "/api/v1/invites/lookup?token="},
		{"a validation error from the service", service.ErrValidation, "/api/v1/invites/lookup?token=whatever"},
		{"a database failure", errors.New("connection reset"), "/api/v1/invites/lookup?token=whatever"},
	}

	var bodies []string
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := handler.NewInviteHandler(&fakeInviteService{lookupErr: tt.err})
			rec := httptest.NewRecorder()

			h.Lookup(rec, httptest.NewRequest(http.MethodGet, tt.url, nil))

			if rec.Code != http.StatusNotFound {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
			}
			bodies = append(bodies, rec.Body.String())
		})
	}

	for i := 1; i < len(bodies); i++ {
		if bodies[i] != bodies[0] {
			t.Errorf("body %d = %q differs from %q; the failures are distinguishable", i, bodies[i], bodies[0])
		}
	}
}

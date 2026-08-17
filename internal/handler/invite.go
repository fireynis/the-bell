package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/service"
)

// InviteService is what the handler needs of the invite service.
//
// As with the vouch handler, none of the rules are restated here: who may
// invite, the shared daily budget, the one-live-invite-per-address rule and the
// uniform 404 on lookup are all the service's, and a copy in this layer would be
// a second place for them to drift.
type InviteService interface {
	Create(ctx context.Context, inviter *domain.User, email, note string) (*service.InviteCreation, error)
	List(ctx context.Context, inviterID string) ([]*domain.Invite, error)
	Revoke(ctx context.Context, id, inviterID string) error
	Lookup(ctx context.Context, rawToken string) (*service.InviteLookup, error)
}

// InviteHandler serves the invitation endpoints.
type InviteHandler struct {
	invites InviteService
	now     func() time.Time
}

func NewInviteHandler(invites InviteService) *InviteHandler {
	return &InviteHandler{invites: invites, now: time.Now}
}

// inviteView is the wire shape of an invitation.
//
// It is a DTO rather than domain.Invite serialized directly, and that is
// load-bearing in two ways. The obvious one is status, which is derived from
// timestamps at read time and so cannot be a stored field. The other is what is
// absent: domain.Invite carries the inviter's id and the id of whoever accepted,
// and neither belongs in a response whose only purpose is to show a member the
// list of people they have invited. There has never been a field for the token,
// at any layer.
type inviteView struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Note      string    `json:"note"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`

	// Both are present only on an accepted invitation, which is the only kind
	// that has them.
	ConsumedAt            *time.Time `json:"consumed_at,omitempty"`
	ConsumedByDisplayName string     `json:"consumed_by_display_name,omitempty"`
}

func newInviteView(invite *domain.Invite, now time.Time) inviteView {
	return inviteView{
		ID:                    invite.ID,
		Email:                 invite.Email,
		Note:                  invite.Note,
		Status:                string(invite.Status(now)),
		CreatedAt:             invite.CreatedAt,
		ExpiresAt:             invite.ExpiresAt,
		ConsumedAt:            invite.ConsumedAt,
		ConsumedByDisplayName: invite.ConsumedByDisplayName,
	}
}

type createInviteRequest struct {
	Email string `json:"email"`
	Note  string `json:"note"`
}

type createInviteResponse struct {
	Invite    inviteView `json:"invite"`
	InviteURL string     `json:"invite_url"`
	EmailSent bool       `json:"email_sent"`
	// EmailError is present only when the invitation could not be emailed. Its
	// absence on a successful send is what lets a client treat the key's
	// presence as "tell the member to pass the link on themselves".
	EmailError string `json:"email_error,omitempty"`
}

// Create handles POST /api/v1/invites.
func (h *InviteHandler) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createInviteRequest
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	creation, err := h.invites.Create(r.Context(), user, req.Email, req.Note)
	if err != nil {
		serviceError(w, err)
		return
	}

	JSON(w, http.StatusCreated, createInviteResponse{
		Invite:     newInviteView(creation.Invite, h.now()),
		InviteURL:  creation.URL,
		EmailSent:  creation.EmailSent,
		EmailError: creation.EmailError,
	})
}

type listInvitesResponse struct {
	Invites []inviteView `json:"invites"`
}

// List handles GET /api/v1/invites, returning the caller's own invitations.
func (h *InviteHandler) List(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	invites, err := h.invites.List(r.Context(), user.ID)
	if err != nil {
		serviceError(w, err)
		return
	}

	now := h.now()
	// Non-nil so an inviter with no invitations gets [] rather than null, which
	// is what emit_empty_slices buys everywhere else in this API.
	views := make([]inviteView, 0, len(invites))
	for _, invite := range invites {
		views = append(views, newInviteView(invite, now))
	}
	JSON(w, http.StatusOK, listInvitesResponse{Invites: views})
}

// Revoke handles DELETE /api/v1/invites/{id}.
//
// Somebody else's invitation is a 404, not a 403, and the service produces that
// answer by making ownership part of the UPDATE rather than a check on a row it
// read first. A 403 would confirm the id names a real invitation.
func (h *InviteHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := requireIDField("invite id", chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.invites.Revoke(r.Context(), id, user.ID); err != nil {
		serviceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type inviteLookupResponse struct {
	Email              string `json:"email"`
	TownName           string `json:"town_name"`
	InviterDisplayName string `json:"inviter_display_name"`
	// Status is always "open" — a lookup that found anything else answered 404.
	// It is sent anyway so the client's invite shape is the same one it handles
	// everywhere else rather than a special case with a field missing.
	Status string `json:"status"`
}

// Lookup handles GET /api/v1/invites/lookup?token=..., unauthenticated.
//
// This is what greets somebody arriving on an invitation link, before they have
// an account of any kind. Every failure is the same bare 404: a missing token,
// an unknown one, and one that was consumed, revoked or has expired are
// indistinguishable, because anything else lets a caller with a list of guesses
// learn which of them were once real.
func (h *InviteHandler) Lookup(w http.ResponseWriter, r *http.Request) {
	lookup, err := h.invites.Lookup(r.Context(), r.URL.Query().Get("token"))
	if err != nil {
		// Not serviceError: that maps ErrValidation and the rest onto distinct
		// statuses and messages, which is exactly the distinction this endpoint
		// must not draw.
		Error(w, http.StatusNotFound, "not found")
		return
	}

	JSON(w, http.StatusOK, inviteLookupResponse{
		Email:              lookup.Email,
		TownName:           lookup.TownName,
		InviterDisplayName: lookup.InviterDisplayName,
		Status:             string(domain.InviteOpen),
	})
}

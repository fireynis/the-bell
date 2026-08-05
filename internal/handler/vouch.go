package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
)

// VouchService defines the operations needed by the vouch handler.
//
// Every trust rule — the vouching threshold, the daily limit, cycle detection,
// duplicate pairs and who may revoke — lives behind these two calls. The
// handler deliberately re-checks none of them: a copy here would be a second
// place for the rules to drift.
type VouchService interface {
	Vouch(ctx context.Context, voucherID, voucheeID string) (*domain.Vouch, error)
	Revoke(ctx context.Context, vouchID, actorID string) error
}

// VouchHandler handles HTTP requests for vouching.
type VouchHandler struct {
	vouches VouchService
}

// NewVouchHandler creates a VouchHandler.
func NewVouchHandler(vouches VouchService) *VouchHandler {
	return &VouchHandler{vouches: vouches}
}

type createVouchRequest struct {
	VoucheeID string `json:"vouchee_id"`
}

// requireIDField trims a caller-supplied identifier and names the field when it
// is missing.
//
// An empty ID has to be rejected here rather than passed down: the service
// would look it up, find nothing, and return ErrNotFound, which reaches the
// caller as 404 "not found" — telling them the person they named does not
// exist when what actually happened is that they never named one.
func requireIDField(field, raw string) (string, error) {
	id := strings.TrimSpace(raw)
	if id == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	return id, nil
}

// Create handles POST /api/v1/vouches.
func (h *VouchHandler) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createVouchRequest
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	voucheeID, err := requireIDField("vouchee_id", req.VoucheeID)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}

	vouch, err := h.vouches.Vouch(r.Context(), user.ID, voucheeID)
	if err != nil {
		serviceError(w, err)
		return
	}

	JSON(w, http.StatusCreated, vouch)
}

// Revoke handles DELETE /api/v1/vouches/{id}.
//
// The caller is passed to the service as the actor rather than being checked
// here, because revocation is not simply "your own vouch": moderators and
// council may revoke anyone's. That rule is the service's to apply.
func (h *VouchHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	vouchID, err := requireIDField("vouch id", chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.vouches.Revoke(r.Context(), vouchID, user.ID); err != nil {
		serviceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

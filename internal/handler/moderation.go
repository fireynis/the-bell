package handler

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/service"
)

// ModerationHandler handles HTTP requests for moderation action operations.
type ModerationHandler struct {
	actions *service.ModerationActionService
}

// NewModerationHandler creates a ModerationHandler.
func NewModerationHandler(actions *service.ModerationActionService) *ModerationHandler {
	return &ModerationHandler{actions: actions}
}

type takeActionRequest struct {
	TargetUserID    string `json:"target_user_id"`
	ActionType      string `json:"action_type"`
	Severity        int    `json:"severity"`
	Reason          string `json:"reason"`
	DurationSeconds *int64 `json:"duration_seconds"`
}

// takeActionOutcome maps a TakeAction result onto the response status.
//
// An action that was persisted before its penalty propagation or enforcement
// failed is still a created action: reporting a failure would tell the
// moderator to retry and duplicate it. Those cases report 201 with partial
// set, so the caller can log the failure without failing the request.
func takeActionOutcome(result *service.TakeActionResult, err error) (status int, partial bool) {
	if err == nil {
		return http.StatusCreated, false
	}
	if result != nil && result.Action != nil {
		return http.StatusCreated, true
	}
	status, _ = statusForError(err)
	return status, false
}

// canQueryByModerator reports whether u may list the actions taken BY a
// moderator. That view exposes which moderator handled which case, so it is
// restricted to the council.
func canQueryByModerator(u *domain.User) bool {
	return u != nil && u.IsCouncil()
}

// TakeAction handles POST /api/v1/moderation/actions.
func (h *ModerationHandler) TakeAction(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req takeActionRequest
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := h.actions.TakeAction(
		r.Context(),
		user.ID,
		req.TargetUserID,
		domain.ActionType(req.ActionType),
		req.Severity,
		req.Reason,
		req.DurationSeconds,
	)
	status, partial := takeActionOutcome(result, err)
	if partial {
		// Either enforcement or penalty propagation failed; the joined error
		// says which.
		slog.Warn("moderation action created but follow-up failed",
			"action_id", result.Action.ID,
			"error", err,
		)
	} else if err != nil {
		serviceError(w, err)
		return
	}

	JSON(w, status, result)
}

// muteStatusResponse is what a moderator may learn about someone else's mute:
// when it ends, and nothing more.
//
// MutedUntil is omitted entirely when the user is not muted, so the field's
// presence is the answer — the same rule ownProfileResponse uses, so a client
// reads both shapes the same way. It is also why this is an object rather than
// a bare timestamp.
type muteStatusResponse struct {
	MutedUntil string `json:"muted_until,omitempty"`
}

// MuteStatus handles GET /api/v1/moderation/users/{user_id}/mute.
//
// This is the only response outside a caller's own profile that carries
// muted_until, and it lives behind the moderator guard for the reason the
// public profile does not carry it at all: a mute is between the user and the
// moderators, and this is the moderators' side of that. Putting it on
// GET /users/{id} would publish it to everyone, that route not even being
// authenticated.
//
// Without it a moderator's only clue would be a past mute action in the audit
// trail, which stays exactly as written after the mute is lifted and would go
// on reporting a mute that no longer exists.
func (h *ModerationHandler) MuteStatus(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// The service re-checks the role; the route guard is the early rejection.
	until, err := h.actions.MuteStatus(r.Context(), user, chi.URLParam(r, "user_id"))
	if err != nil {
		serviceError(w, err)
		return
	}

	var resp muteStatusResponse
	if until != nil {
		resp.MutedUntil = until.Format(timestampFormat)
	}
	JSON(w, http.StatusOK, resp)
}

// LiftMute handles DELETE /api/v1/moderation/users/{user_id}/mute.
//
// A DELETE on the mute rather than a POST of an "unmute" action, because that
// is what it is: the mute is a value on the user (muted_until), and this
// removes it. Modelling it as an action would imply a moderation_actions row,
// which LiftMute deliberately does not write — see its doc comment.
//
// It answers 204 with no body, including for a user who was not muted. The
// service treats that as done rather than as an error, the same as removing a
// reaction that was never left, and the status has to say the same thing or the
// endpoint is lying about what it did.
func (h *ModerationHandler) LiftMute(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// The service re-checks the role; the route guard is the early rejection.
	if err := h.actions.LiftMute(r.Context(), user, chi.URLParam(r, "user_id")); err != nil {
		serviceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type listActionsResponse struct {
	Actions []service.ActionHistoryEntry `json:"actions"`
}

// ListActions handles GET /api/v1/moderation/actions/{user_id}.
func (h *ModerationHandler) ListActions(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	targetUserID := chi.URLParam(r, "user_id")
	byModerator := r.URL.Query().Get("role") == "moderator"

	if byModerator && !canQueryByModerator(user) {
		Error(w, http.StatusForbidden, "council role required")
		return
	}

	limit := parseLimit(r.URL.Query().Get("limit"))
	offset := parseOffset(r.URL.Query().Get("offset"))

	entries, err := h.actions.GetActionHistory(r.Context(), targetUserID, byModerator, limit, offset)
	if err != nil {
		serviceError(w, err)
		return
	}

	if entries == nil {
		entries = []service.ActionHistoryEntry{}
	}

	JSON(w, http.StatusOK, listActionsResponse{Actions: entries})
}

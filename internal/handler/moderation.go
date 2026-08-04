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

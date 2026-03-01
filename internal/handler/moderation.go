package handler

import (
	"log/slog"
	"net/http"

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
	if err != nil {
		// If we got a partial result (action created but penalties failed),
		// still return the result with 201 but log the penalty failure.
		if result != nil && result.Action != nil {
			slog.Warn("penalty propagation failed after action created",
				"action_id", result.Action.ID,
				"error", err,
			)
			JSON(w, http.StatusCreated, result)
			return
		}
		serviceError(w, err)
		return
	}

	JSON(w, http.StatusCreated, result)
}

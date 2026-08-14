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

// suspensionStatusResponse is the suspension's counterpart to
// muteStatusResponse, field-for-field: the expiry when one is in force, and an
// empty object otherwise.
type suspensionStatusResponse struct {
	SuspendedUntil string `json:"suspended_until,omitempty"`
}

// SuspensionStatus handles GET /api/v1/moderation/users/{user_id}/suspension.
//
// The moderator's view of whether somebody is suspended right now.
// GET /api/v1/users/{id} carries is_active, which does read false during a
// suspension — but it reads false for a deactivated account too, and it never
// says when the suspension ends, so it cannot tell a moderator whether an early
// lift is worth offering or how much time it would actually save.
//
// Like MuteStatus and unlike the DELETE below, this does not refuse a
// self-query.
func (h *ModerationHandler) SuspensionStatus(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// The service re-checks the role; the route guard is the early rejection.
	until, err := h.actions.SuspensionStatus(r.Context(), user, chi.URLParam(r, "user_id"))
	if err != nil {
		serviceError(w, err)
		return
	}

	var resp suspensionStatusResponse
	if until != nil {
		resp.SuspendedUntil = until.Format(timestampFormat)
	}
	JSON(w, http.StatusOK, resp)
}

// LiftSuspension handles DELETE /api/v1/moderation/users/{user_id}/suspension.
//
// A DELETE of the suspension for the reason LiftMute is a DELETE of the mute:
// the suspension is a value on the user (suspended_until) and this removes it.
// Modelling it as an action would imply a moderation_actions row, which the
// service deliberately does not write.
//
// It answers 204 with no body, including for a user who was not suspended.
func (h *ModerationHandler) LiftSuspension(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// The service re-checks the role; the route guard is the early rejection.
	if err := h.actions.LiftSuspension(r.Context(), user, chi.URLParam(r, "user_id")); err != nil {
		serviceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ownPenaltyResponse is the trust cost of one action to the member it landed
// on. `decays_at` is omitted when the penalty never decays, which is readable
// as "never" rather than "unknown" only because the enclosing object is present
// at all — a member who took no penalty has no `penalty` key.
type ownPenaltyResponse struct {
	Amount   float64 `json:"amount"`
	DecaysAt string  `json:"decays_at,omitempty"`
}

// ownModerationEntryResponse is one moderation action as its subject sees it.
//
// It carries no moderator, by the same rule the mute and suspension lifts on a
// member's profile follow: which moderator handled a case appears on no
// member-facing response. In a town small enough to run this platform, naming
// the individual turns a moderation decision into a personal grievance with a
// neighbour; the decision belongs to the moderation team.
//
// service.OwnModerationEntry has already stripped the record, so this is a
// second wall rather than the only one: there is no field here for a moderator
// id, and there is none there either.
type ownModerationEntryResponse struct {
	ID        string              `json:"id"`
	Action    string              `json:"action"`
	Severity  int                 `json:"severity"`
	Reason    string              `json:"reason"`
	CreatedAt string              `json:"created_at"`
	ExpiresAt string              `json:"expires_at,omitempty"`
	Penalty   *ownPenaltyResponse `json:"penalty,omitempty"`
}

type ownModerationHistoryResponse struct {
	Actions []ownModerationEntryResponse `json:"actions"`
}

func toOwnModerationEntry(e service.OwnModerationEntry) ownModerationEntryResponse {
	resp := ownModerationEntryResponse{
		ID:        e.ID,
		Action:    string(e.Action),
		Severity:  e.Severity,
		Reason:    e.Reason,
		CreatedAt: e.CreatedAt.Format(timestampFormat),
	}
	if e.ExpiresAt != nil {
		resp.ExpiresAt = e.ExpiresAt.Format(timestampFormat)
	}
	if e.Penalty != nil {
		penalty := &ownPenaltyResponse{Amount: e.Penalty.Amount}
		if e.Penalty.DecaysAt != nil {
			penalty.DecaysAt = e.Penalty.DecaysAt.Format(timestampFormat)
		}
		resp.Penalty = penalty
	}
	return resp
}

// OwnHistory handles GET /api/v1/users/me/moderation-history.
//
// The member-facing counterpart to ListActions below, and the only moderation
// history a member has ever been able to read about themselves beyond a lifted
// mute. Before it, somebody warned for something learned that they had been
// warned only if a moderator told them out of band — the reason, the trust it
// cost and when that cost fades all sat behind the moderator-only route.
//
// It is served by the moderation handler rather than the user handler because
// the shape and the policy are moderation's, not the profile's. The handler is
// named for the subject matter; the route's guard decides the audience.
//
// The subject is the authenticated caller and is taken from the session, never
// from the URL or a query parameter. That is the whole of the authorization:
// there is no id to tamper with, so there is no way to ask this route about
// anybody else. It is also why the route needs no role floor — every member is
// entitled to their own record, and to nobody else's.
//
// It deliberately sits behind a guard that skips RequireActive; see the route
// registration in internal/server/routes.go. A suspended or banned member is
// exactly who most needs to read why.
//
// A clean record answers 200 with an empty array rather than 404: "nothing has
// happened to you" is a successful answer to the question, and the frontend has
// a reassuring empty state to render for it.
func (h *ModerationHandler) OwnHistory(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	entries, err := h.actions.OwnModerationHistory(
		r.Context(),
		user.ID,
		parseLimit(r.URL.Query().Get("limit")),
		parseOffset(r.URL.Query().Get("offset")),
	)
	if err != nil {
		serviceError(w, err)
		return
	}

	resp := ownModerationHistoryResponse{Actions: make([]ownModerationEntryResponse, 0, len(entries))}
	for _, e := range entries {
		resp.Actions = append(resp.Actions, toOwnModerationEntry(e))
	}

	JSON(w, http.StatusOK, resp)
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

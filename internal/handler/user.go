package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
)

// timestampFormat is the wire format for every timestamp this handler emits.
// The frontend parses profile and vouch timestamps with the same code, so the
// two must not drift.
const timestampFormat = "2006-01-02T15:04:05Z07:00"

// UserHandler handles HTTP requests for user profile operations.
type UserHandler struct {
	users   profileService
	posts   authorPostLister
	vouches VouchLister
	lifts   reliefLister
}

// reliefLister abstracts reading the lifts a member is entitled to see about
// themselves — mutes and suspensions a moderator ended early.
type reliefLister interface {
	MuteLifts(ctx context.Context, userID string, limit int) ([]domain.ModerationRelief, error)
	SuspensionLifts(ctx context.Context, userID string, limit int) ([]domain.ModerationRelief, error)
}

// SetReliefLister supplies the moderation relief reader.
//
// A setter rather than a constructor parameter, matching
// VouchService.SetPenaltyRepository: the handler predates this and every other
// field is independent of it, so a deployment that never wires one still serves
// profiles rather than failing to construct.
func (h *UserHandler) SetReliefLister(l reliefLister) { h.lifts = l }

// maxOwnLifts bounds the self view, per relief type. This is a member's own
// record of being released, not an audit log — enough to answer "was my mute
// lifted?" without making the profile read unbounded.
const maxOwnLifts = 10

// profileService abstracts the user profile reads and writes the handler needs.
type profileService interface {
	GetByID(ctx context.Context, id string) (*domain.User, error)
	UpdateProfile(ctx context.Context, id, displayName, bio, avatarURL string) (*domain.User, error)
}

// authorPostLister abstracts listing a single author's posts.
type authorPostLister interface {
	ListByAuthor(ctx context.Context, authorID string, limit int) ([]*domain.Post, error)
}

// VouchLister abstracts reading vouches for profile display.
type VouchLister interface {
	ListReceivedVouches(ctx context.Context, userID string) ([]*domain.Vouch, error)
	ListGivenVouches(ctx context.Context, userID string) ([]*domain.Vouch, error)
}

// NewUserHandler creates a UserHandler.
func NewUserHandler(users profileService, posts authorPostLister, vouches VouchLister) *UserHandler {
	return &UserHandler{users: users, posts: posts, vouches: vouches}
}

type updateProfileRequest struct {
	DisplayName string `json:"display_name"`
	Bio         string `json:"bio"`
	AvatarURL   string `json:"avatar_url"`
}

type userProfileResponse struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"display_name"`
	Bio         string  `json:"bio"`
	AvatarURL   string  `json:"avatar_url"`
	TrustScore  float64 `json:"trust_score"`
	Role        string  `json:"role"`
	IsActive    bool    `json:"is_active"`
	JoinedAt    string  `json:"joined_at"`
}

// ownProfileResponse is the profile a user sees of themselves. It carries the
// moderation state the user is entitled to know about but nobody else is.
type ownProfileResponse struct {
	userProfileResponse
	// MutedUntil is omitted entirely when the user is not muted, so the field's
	// presence is the answer to "am I muted?".
	MutedUntil string `json:"muted_until,omitempty"`
	// MuteLifts is the record of moderators ending a mute early. Omitted when
	// empty, on the same principle as MutedUntil.
	//
	// This is the only moderation history a member sees about themselves: the
	// actions taken against them all sit behind /v1/moderation, which requires
	// the moderator role. That asymmetry is deliberate for now — showing a
	// member their own severities, penalties and the moderators who applied
	// them is a policy question in its own right — but a release had to be
	// visible to the person released, or it may as well not have happened.
	MuteLifts []muteLiftResponse `json:"mute_lifts,omitempty"`
	// SuspensionLifts is the same record for suspensions, and it is the only
	// trace of one a member can ever see. A suspension surfaces to them as
	// is_active being false; lifting it just makes that revert, so without this
	// a member released early cannot tell an early release from a suspension
	// that ran its full course.
	SuspensionLifts []suspensionLiftResponse `json:"suspension_lifts,omitempty"`
}

// muteLiftResponse is one mute lift as its subject sees it.
//
// It names no moderator. Which moderator acted appears on no member-facing
// response today, and changing that is a policy decision rather than a side
// effect of adding this field. The member learns that they were released and
// what the mute would otherwise have run to, which is the whole of what the
// record is for.
type muteLiftResponse struct {
	LiftedAt string `json:"lifted_at"`
	// PreviousMutedUntil is when the mute would have ended had it run its
	// course. Omitted when the record has none.
	PreviousMutedUntil string `json:"previous_muted_until,omitempty"`
}

// suspensionLiftResponse is one suspension lift as its subject sees it. It
// names no moderator either, and for the same reason.
//
// The field is previous_suspended_until rather than a shared name because the
// member's profile already speaks in muted_until and suspended_until, and a
// single previous_expires_at across both lists would make the reader work out
// which restriction each entry came from.
type suspensionLiftResponse struct {
	LiftedAt string `json:"lifted_at"`
	// PreviousSuspendedUntil is when the suspension would have ended had it run
	// its course. Omitted when the record has none.
	PreviousSuspendedUntil string `json:"previous_suspended_until,omitempty"`
}

func toMuteLiftResponses(reliefs []domain.ModerationRelief) []muteLiftResponse {
	if len(reliefs) == 0 {
		return nil
	}
	out := make([]muteLiftResponse, 0, len(reliefs))
	for _, r := range reliefs {
		lift := muteLiftResponse{LiftedAt: r.CreatedAt.Format(timestampFormat)}
		if r.PreviousExpiresAt != nil {
			lift.PreviousMutedUntil = r.PreviousExpiresAt.Format(timestampFormat)
		}
		out = append(out, lift)
	}
	return out
}

func toSuspensionLiftResponses(reliefs []domain.ModerationRelief) []suspensionLiftResponse {
	if len(reliefs) == 0 {
		return nil
	}
	out := make([]suspensionLiftResponse, 0, len(reliefs))
	for _, r := range reliefs {
		lift := suspensionLiftResponse{LiftedAt: r.CreatedAt.Format(timestampFormat)}
		if r.PreviousExpiresAt != nil {
			lift.PreviousSuspendedUntil = r.PreviousExpiresAt.Format(timestampFormat)
		}
		out = append(out, lift)
	}
	return out
}

func toProfileResponse(u *domain.User) userProfileResponse {
	return userProfileResponse{
		ID:          u.ID,
		DisplayName: u.DisplayName,
		Bio:         u.Bio,
		AvatarURL:   u.AvatarURL,
		TrustScore:  u.TrustScore,
		Role:        string(u.Role),
		IsActive:    u.IsActive,
		JoinedAt:    u.JoinedAt.Format(timestampFormat),
	}
}

// toOwnProfileResponse builds the self view. A mute is between the user and the
// moderators: the user must be able to see why they cannot post, and no other
// caller has any business knowing, so muted_until appears here and in no other
// response shape.
//
// An expired mute is reported as no mute at all rather than as a past
// timestamp, so the client needs no clock of its own to interpret the field.
func toOwnProfileResponse(u *domain.User, now time.Time) ownProfileResponse {
	resp := ownProfileResponse{userProfileResponse: toProfileResponse(u)}
	if u.IsMuted(now) {
		resp.MutedUntil = u.MutedUntil.Format(timestampFormat)
	}
	return resp
}

// ownProfileWithLifts builds the self view and attaches the member's mute and
// suspension lifts.
//
// A failed read is an error rather than an empty list. "No lifts" is a definite
// answer to "was I ever released?", and returning it when the truth is unknown
// is the same silent wrong answer that made a log-line-only record inadequate
// in the first place. Each read is one indexed lookup on a small table, so a
// failure here means the database is unavailable and the rest of the profile is
// in no better shape.
func (h *UserHandler) ownProfileWithLifts(ctx context.Context, u *domain.User) (ownProfileResponse, error) {
	resp := toOwnProfileResponse(u, time.Now())
	if h.lifts == nil {
		return resp, nil
	}

	muteLifts, err := h.lifts.MuteLifts(ctx, u.ID, maxOwnLifts)
	if err != nil {
		return ownProfileResponse{}, err
	}
	suspensionLifts, err := h.lifts.SuspensionLifts(ctx, u.ID, maxOwnLifts)
	if err != nil {
		return ownProfileResponse{}, err
	}
	resp.MuteLifts = toMuteLiftResponses(muteLifts)
	resp.SuspensionLifts = toSuspensionLiftResponses(suspensionLifts)
	return resp, nil
}

// GetMe handles GET /api/v1/users/me.
func (h *UserHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	resp, err := h.ownProfileWithLifts(r.Context(), user)
	if err != nil {
		serviceError(w, err)
		return
	}

	JSON(w, http.StatusOK, resp)
}

// UpdateMe handles PUT /api/v1/users/me.
func (h *UserHandler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req updateProfileRequest
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	updated, err := h.users.UpdateProfile(r.Context(), user.ID, req.DisplayName, req.Bio, req.AvatarURL)
	if err != nil {
		serviceError(w, err)
		return
	}

	// Also a self view, so it carries the same moderation state GetMe does.
	resp, err := h.ownProfileWithLifts(r.Context(), updated)
	if err != nil {
		serviceError(w, err)
		return
	}

	JSON(w, http.StatusOK, resp)
}

// GetByID handles GET /api/v1/users/{id}.
func (h *UserHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	user, err := h.users.GetByID(r.Context(), id)
	if err != nil {
		serviceError(w, err)
		return
	}

	JSON(w, http.StatusOK, toProfileResponse(user))
}

type listUserPostsResponse struct {
	Posts []*domain.Post `json:"posts"`
}

// ListPosts handles GET /api/v1/users/{id}/posts.
func (h *UserHandler) ListPosts(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	limit := parseLimit(r.URL.Query().Get("limit"))

	posts, err := h.posts.ListByAuthor(r.Context(), id, limit)
	if err != nil {
		serviceError(w, err)
		return
	}

	if posts == nil {
		posts = []*domain.Post{}
	}

	JSON(w, http.StatusOK, listUserPostsResponse{Posts: posts})
}

// vouchEntry is one vouch on a profile's list.
//
// The display names are sent as the empty string rather than omitted when a
// member has not set one: the list renders both parties, so the key is always
// meaningful, and a client falling back to the id needs no separate rule for
// "absent" and "blank". This is the one place the two names are shown together,
// which is why it carries both rather than only the counterpart's.
type vouchEntry struct {
	ID                 string `json:"id"`
	VoucherID          string `json:"voucher_id"`
	VoucherDisplayName string `json:"voucher_display_name"`
	VoucheeID          string `json:"vouchee_id"`
	VoucheeDisplayName string `json:"vouchee_display_name"`
	Status             string `json:"status"`
	CreatedAt          string `json:"created_at"`
}

type listVouchesResponse struct {
	Received []vouchEntry `json:"received"`
	Given    []vouchEntry `json:"given"`
}

func toVouchEntry(v *domain.Vouch) vouchEntry {
	return vouchEntry{
		ID:                 v.ID,
		VoucherID:          v.VoucherID,
		VoucherDisplayName: v.VoucherDisplayName,
		VoucheeID:          v.VoucheeID,
		VoucheeDisplayName: v.VoucheeDisplayName,
		Status:             string(v.Status),
		CreatedAt:          v.CreatedAt.Format(timestampFormat),
	}
}

// ListVouches handles GET /api/v1/users/{id}/vouches.
func (h *UserHandler) ListVouches(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	received, err := h.vouches.ListReceivedVouches(r.Context(), id)
	if err != nil {
		serviceError(w, err)
		return
	}

	given, err := h.vouches.ListGivenVouches(r.Context(), id)
	if err != nil {
		serviceError(w, err)
		return
	}

	resp := listVouchesResponse{
		Received: make([]vouchEntry, 0, len(received)),
		Given:    make([]vouchEntry, 0, len(given)),
	}
	for _, v := range received {
		resp.Received = append(resp.Received, toVouchEntry(v))
	}
	for _, v := range given {
		resp.Given = append(resp.Given, toVouchEntry(v))
	}

	JSON(w, http.StatusOK, resp)
}

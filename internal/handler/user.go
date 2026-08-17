package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/service"
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
	ListDirectory(ctx context.Context, query string, limit, offset int) ([]*domain.User, int, error)
	UpdateResidencyClaim(ctx context.Context, id, claim string) error
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
	// It is here rather than on GET /v1/users/me/moderation-history, which is
	// where a member now reads the actions taken against them, because a lift
	// is not one: it writes no moderation_actions row, carries no severity and
	// costs no trust. Putting mercy in the list of sanctions would misfile it.
	// The two are read together — the history says what was done, this says
	// what was undone — and neither names the moderator who did either.
	MuteLifts []muteLiftResponse `json:"mute_lifts,omitempty"`
	// SuspensionLifts is the same record for suspensions, and it is the only
	// trace of one a member can ever see. A suspension surfaces to them as
	// is_active being false; lifting it just makes that revert, so without this
	// a member released early cannot tell an early release from a suspension
	// that ran its full course.
	SuspensionLifts []suspensionLiftResponse `json:"suspension_lifts,omitempty"`
	// ResidencyClaim is what this user told the council about where they live.
	//
	// It is here for one concrete reason: the field that collects it has to
	// prefill with what they said last time. Without this a resident who
	// returned to their profile in a new session saw an empty box and had to
	// retype their address to change one word of it, or worse, assumed the
	// claim had been lost.
	//
	// It appears on the self view and on the council's approval queue, and on
	// nothing else. The two readers are the only two who have a reason to see
	// it: the person who wrote it, and the people deciding whether to admit
	// them. Note where it is NOT — it is on ownProfileResponse rather than on
	// the embedded userProfileResponse, so the public profile and the directory
	// cannot acquire it by sharing that type.
	//
	// Always present, empty string included, unlike the moderation fields above.
	// Those use presence to mean something ("am I muted?"); this is a text box's
	// contents, and a client prefilling it should not have to treat "absent" and
	// "blank" as two cases.
	ResidencyClaim string `json:"residency_claim"`
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
// response shape. The residency claim is the same shape of thing from the other
// direction — the user's own words, readable by them and by the council
// reviewing them, and by nobody else.
//
// An expired mute is reported as no mute at all rather than as a past
// timestamp, so the client needs no clock of its own to interpret the field.
func toOwnProfileResponse(u *domain.User, now time.Time) ownProfileResponse {
	resp := ownProfileResponse{
		userProfileResponse: toProfileResponse(u),
		ResidencyClaim:      u.ResidencyClaim,
	}
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

type residencyClaimRequest struct {
	Claim string `json:"claim"`
}

// UpdateResidencyClaim handles PUT /api/v1/users/me/residency-claim.
//
// It answers 204 with no body, and that is the whole contract. The claim is
// never read back through the API by the person who wrote it, because there is
// nothing to read back that the client did not just send — and it is never
// returned to anyone else, because the only reader is the council's approval
// queue. A response echoing the claim would create a second place it appears on
// the wire for no benefit.
//
// The route is guarded by auth and RequireActive with no role floor, which is
// exactly right for who uses it: a pending resident is active — that is what
// makes their application reviewable — and a member who moves house has to be
// able to correct what they said. Only banned and suspended accounts are shut
// out, and neither has an application in front of the council.
func (h *UserHandler) UpdateResidencyClaim(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req residencyClaimRequest
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.users.UpdateResidencyClaim(r.Context(), user.ID, req.Claim); err != nil {
		serviceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
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

// directoryEntry is one neighbour as the directory shows them.
//
// It is deliberately four fields. The directory is a browsable list of everyone
// in town, readable by any signed-in resident including a pending one, so it
// carries only what a person needs to recognise a neighbour and open their
// profile: who they are, where they stand, and how new they are. Trust score,
// mute state and is_active are all readable elsewhere by callers who have a
// reason to ask for one person — putting them here would publish the town's
// entire moderation posture in a single unauthenticated-adjacent request.
//
// display_name is sent as the empty string rather than omitted when a resident
// has not set one, matching the vouch listings: the key is always present, so a
// client falls back to the id for anything falsy.
type directoryEntry struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	JoinedAt    string `json:"joined_at"`
}

// listDirectoryResponse pairs the page with the size of the whole match, which
// is what lets a client render a pager rather than discovering the end by
// walking off it.
type listDirectoryResponse struct {
	Users []directoryEntry `json:"users"`
	Total int              `json:"total"`
}

// parseDirectoryLimit mirrors parseLimit with the directory's own default. The
// feed's 20 answers a different question — how many posts fill a screen — and
// the directory's page size is part of its published contract.
//
// The council's approval queue parses its limit through here too: both are
// searchable listings of users and they publish the same bounds, so the two
// endpoints clamp identically by construction rather than by remembering.
func parseDirectoryLimit(s string) int {
	if s == "" {
		return service.DirectoryDefaultLimit
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return service.DirectoryDefaultLimit
	}
	if n > service.DirectoryMaxLimit {
		return service.DirectoryMaxLimit
	}
	return n
}

// ListDirectory handles GET /api/v1/users.
func (h *UserHandler) ListDirectory(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	users, total, err := h.users.ListDirectory(r.Context(),
		query.Get("q"),
		parseDirectoryLimit(query.Get("limit")),
		parseOffset(query.Get("offset")),
	)
	if err != nil {
		serviceError(w, err)
		return
	}

	entries := make([]directoryEntry, 0, len(users))
	for _, u := range users {
		entries = append(entries, directoryEntry{
			ID:          u.ID,
			DisplayName: u.DisplayName,
			Role:        string(u.Role),
			JoinedAt:    u.JoinedAt.Format(timestampFormat),
		})
	}

	JSON(w, http.StatusOK, listDirectoryResponse{Users: entries, Total: total})
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

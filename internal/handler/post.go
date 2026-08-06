package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/fireynis/the-bell/internal/storage"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

// ReactionEnricher loads batch reaction data for posts.
type ReactionEnricher interface {
	BatchCountByPosts(ctx context.Context, postIDs []string) (map[string]map[domain.ReactionType]int, error)
	BatchGetUserReactions(ctx context.Context, userID string, postIDs []string) (map[string][]domain.ReactionType, error)
}

// PostHandlerOption configures a PostHandler.
type PostHandlerOption func(*PostHandler)

// WithStorage attaches a Storage backend for image uploads.
func WithStorage(s storage.Storage) PostHandlerOption {
	return func(h *PostHandler) { h.store = s }
}

// WithReactionEnricher attaches a ReactionEnricher for feed enrichment.
func WithReactionEnricher(re ReactionEnricher) PostHandlerOption {
	return func(h *PostHandler) { h.reactionEnricher = re }
}

// PostEventPublisher publishes post events for real-time SSE delivery.
type PostEventPublisher interface {
	PublishPost(ctx context.Context, postJSON []byte) error
}

// WithPostPublisher attaches an event publisher for SSE post events.
func WithPostPublisher(pub PostEventPublisher) PostHandlerOption {
	return func(h *PostHandler) { h.publisher = pub }
}

// PostHandler handles HTTP requests for post operations.
type PostHandler struct {
	posts            *service.PostService
	store            storage.Storage
	reactionEnricher ReactionEnricher
	publisher        PostEventPublisher
}

// NewPostHandler creates a PostHandler.
func NewPostHandler(posts *service.PostService, opts ...PostHandlerOption) *PostHandler {
	h := &PostHandler{posts: posts}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

type createPostRequest struct {
	Body      string `json:"body"`
	ImagePath string `json:"image_path,omitempty"`
}

type updatePostRequest struct {
	Body string `json:"body"`
}

type listFeedResponse struct {
	Posts      []*domain.Post `json:"posts"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

// Create handles POST /api/v1/posts.
// It accepts either application/json or multipart/form-data.
// For multipart requests the "body" form field supplies the post text and
// an optional "image" file field supplies an image to upload.
func (h *PostHandler) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// The early rejection, so a muted or low-trust user is turned away before
	// their upload is parsed. PostService.Create checks again; that one is
	// authoritative.
	if !user.CanPost(time.Now()) {
		Error(w, http.StatusForbidden, "posting not allowed")
		return
	}

	var req createPostRequest

	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := h.parseMultipartCreate(r, &req); err != nil {
			if errors.Is(err, errUnsupportedType) || errors.Is(err, errFileTooLarge) {
				Error(w, http.StatusBadRequest, err.Error())
			} else {
				Error(w, http.StatusBadRequest, fmt.Sprintf("invalid multipart request: %v", err))
			}
			return
		}
	} else {
		if err := Decode(r, &req); err != nil {
			Error(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	post, err := h.posts.Create(r.Context(), user, req.Body, req.ImagePath)
	if err != nil {
		serviceError(w, err)
		return
	}

	// Publish for SSE.
	if h.publisher != nil {
		if data, err := json.Marshal(post); err == nil {
			_ = h.publisher.PublishPost(r.Context(), data)
		}
	}

	JSON(w, http.StatusCreated, post)
}

// parseMultipartCreate parses a multipart/form-data request into a createPostRequest.
// If an "image" file part is present it is validated, saved to storage, and the
// resulting path is set on req.ImagePath.
func (h *PostHandler) parseMultipartCreate(r *http.Request, req *createPostRequest) error {
	// Limit total request body to maxImageSize + 1 MB overhead for form fields.
	r.Body = http.MaxBytesReader(nil, r.Body, maxImageSize+1<<20)

	if err := r.ParseMultipartForm(maxImageSize); err != nil {
		return fmt.Errorf("parsing multipart form: %w", err)
	}

	req.Body = r.FormValue("body")

	imgData, ext, err := parseImageUpload(r, maxImageSize)
	if err != nil {
		return err
	}
	// No image field present — text-only post.
	if imgData == nil {
		return nil
	}

	if h.store == nil {
		return fmt.Errorf("image uploads not configured")
	}

	filename := fmt.Sprintf("%s%s", mustUUIDv7(), ext)
	path, err := h.store.Save(r.Context(), filename, bytes.NewReader(imgData))
	if err != nil {
		return fmt.Errorf("saving image: %w", err)
	}

	req.ImagePath = h.store.URL(path)
	return nil
}

func mustUUIDv7() string {
	id, err := uuid.NewV7()
	if err != nil {
		// uuid.NewV7 only fails if the random source is broken.
		panic(fmt.Sprintf("generating UUIDv7: %v", err))
	}
	return id.String()
}

// canViewPost reports whether viewer may read post.
//
// A removed post stays readable to its author and to moderators — the author
// needs to know it is gone, and moderators review reports filed against it,
// including reports on posts the author deleted after being reported. To
// everyone else it must be indistinguishable from a post that never existed.
//
// The caller answers a refusal with 404, never 403. A 403 confirms that the id
// names a real post, which is exactly the fact being withheld; anyone holding
// the id held it because the post was public while it was live. Do not
// "improve" this to 403 for clarity — the identical 404 is the point.
//
// A nil viewer is an anonymous reader, not an error: this endpoint is public.
func canViewPost(post *domain.Post, viewer *domain.User) bool {
	if post == nil {
		return false
	}
	if post.Status == domain.PostVisible {
		return true
	}
	if viewer == nil {
		return false
	}
	// CanModerate covers moderators and council, and requires an active
	// account; a suspended moderator gets the ordinary reader's view.
	return viewer.ID == post.AuthorID || viewer.CanModerate()
}

// GetByID handles GET /api/v1/posts/{id}.
func (h *PostHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	post, err := h.posts.GetByID(r.Context(), id)
	if err != nil {
		serviceError(w, err)
		return
	}

	// UserFromContext is populated by middleware.OptionalAuth on this route;
	// an anonymous reader simply yields nil.
	viewer, _ := middleware.UserFromContext(r.Context())
	if !canViewPost(post, viewer) {
		// Byte-identical to the not-found response serviceError produces for a
		// post that does not exist, so the two cases cannot be told apart.
		Error(w, http.StatusNotFound, "not found")
		return
	}

	h.enrichPosts(r.Context(), []*domain.Post{post})

	JSON(w, http.StatusOK, post)
}

// ListFeed handles GET /api/v1/posts.
func (h *PostHandler) ListFeed(w http.ResponseWriter, r *http.Request) {
	cursor := r.URL.Query().Get("cursor")
	limit := parseLimit(r.URL.Query().Get("limit"))

	posts, err := h.posts.ListFeed(r.Context(), cursor, limit)
	if err != nil {
		serviceError(w, err)
		return
	}

	if posts == nil {
		posts = []*domain.Post{}
	}

	h.enrichPosts(r.Context(), posts)

	JSON(w, http.StatusOK, listFeedResponse{Posts: posts, NextCursor: nextCursor(posts, limit)})
}

// Update handles PATCH /api/v1/posts/{id}.
func (h *PostHandler) Update(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := chi.URLParam(r, "id")

	var req updatePostRequest
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	post, err := h.posts.UpdateBody(r.Context(), id, user.ID, req.Body)
	if err != nil {
		serviceError(w, err)
		return
	}

	JSON(w, http.StatusOK, post)
}

// Delete handles DELETE /api/v1/posts/{id}.
func (h *PostHandler) Delete(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := chi.URLParam(r, "id")

	if err := h.posts.Delete(r.Context(), id, user.ID); err != nil {
		serviceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type removePostRequest struct {
	Reason string `json:"reason"`
}

// RemoveByModerator handles POST /api/v1/moderation/posts/{id}/remove.
//
// It answers 204 and deliberately returns no body. The reason the moderator
// just typed is a private note (see domain.Post.RemovalReason), and echoing it
// back would make this the first response on the wire to carry one — the exact
// leak that put `json:"-"` on the field. A moderator UI that needs to read
// reasons back must ask for them through a response type that says so.
func (h *PostHandler) RemoveByModerator(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req removePostRequest
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// The service re-checks the role; the route guard is the early rejection.
	if err := h.posts.RemoveByModerator(r.Context(), user, chi.URLParam(r, "id"), req.Reason); err != nil {
		serviceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// nextCursor returns the cursor for the page following posts, or "" when this
// page is the last one.
//
// A full page is the only signal the feed query gives that more posts may
// exist, so exactly limit posts always yields a cursor — even when it turns out
// the next page is empty.
func nextCursor(posts []*domain.Post, limit int) string {
	if len(posts) == 0 || len(posts) != limit {
		return ""
	}
	return posts[len(posts)-1].ID
}

// enrichPosts attaches reaction counts and user reactions to posts.
//
// Enrichment is best-effort: a feed missing its reaction counts is still a
// usable feed, so a failure is logged rather than failing the whole request.
func (h *PostHandler) enrichPosts(ctx context.Context, posts []*domain.Post) {
	if h.reactionEnricher == nil || len(posts) == 0 {
		return
	}

	postIDs := make([]string, len(posts))
	for i, p := range posts {
		postIDs[i] = p.ID
	}

	counts, err := h.reactionEnricher.BatchCountByPosts(ctx, postIDs)
	if err != nil {
		slog.Warn("loading reaction counts for feed", "error", err, "posts", len(postIDs))
	} else {
		for _, p := range posts {
			if c, ok := counts[p.ID]; ok {
				p.ReactionCounts = c
			}
		}
	}

	user, ok := middleware.UserFromContext(ctx)
	if !ok {
		return
	}

	userReactions, err := h.reactionEnricher.BatchGetUserReactions(ctx, user.ID, postIDs)
	if err != nil {
		slog.Warn("loading user reactions for feed", "error", err, "user_id", user.ID)
		return
	}
	for _, p := range posts {
		if ur, ok := userReactions[p.ID]; ok {
			p.UserReactions = ur
		}
	}
}

func parseLimit(s string) int {
	if s == "" {
		return defaultLimit
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return defaultLimit
	}
	if n > maxLimit {
		return maxLimit
	}
	return n
}

func parseOffset(s string) int {
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

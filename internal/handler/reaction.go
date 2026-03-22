package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/service"
)

// ReactionEventPublisher publishes reaction events for SSE notifications.
// The sse package doesn't exist yet, so this interface is defined generically.
type ReactionEventPublisher interface {
	PublishReactionEvent(ctx context.Context, postID, postAuthorID, reactionType, reactorID string) error
}

// ReactionHandlerOption configures a ReactionHandler.
type ReactionHandlerOption func(*ReactionHandler)

// WithReactionPublisher attaches an SSE event publisher.
func WithReactionPublisher(pub ReactionEventPublisher) ReactionHandlerOption {
	return func(h *ReactionHandler) { h.publisher = pub }
}

// ReactionHandler handles HTTP requests for reaction operations.
type ReactionHandler struct {
	reactions *service.ReactionService
	posts     *service.PostService
	publisher ReactionEventPublisher
}

// NewReactionHandler creates a ReactionHandler.
func NewReactionHandler(reactions *service.ReactionService, posts *service.PostService, opts ...ReactionHandlerOption) *ReactionHandler {
	h := &ReactionHandler{reactions: reactions, posts: posts}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

type addReactionRequest struct {
	Type string `json:"type"`
}

// Add handles POST /api/v1/posts/{postId}/reactions.
func (h *ReactionHandler) Add(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	postID := chi.URLParam(r, "postId")
	if postID == "" {
		Error(w, http.StatusBadRequest, "missing post ID")
		return
	}

	var req addReactionRequest
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	reaction, err := h.reactions.Add(r.Context(), user.ID, postID, domain.ReactionType(req.Type))
	if err != nil {
		if errors.Is(err, service.ErrInvalidReactionType) {
			Error(w, http.StatusBadRequest, err.Error())
			return
		}
		Error(w, http.StatusInternalServerError, "failed to add reaction")
		return
	}

	// Publish reaction event for SSE notification (if publisher is wired).
	if h.publisher != nil && h.posts != nil {
		if post, err := h.posts.GetByID(r.Context(), postID); err == nil {
			_ = h.publisher.PublishReactionEvent(r.Context(), postID, post.AuthorID, string(reaction.Type), user.ID)
		}
	}

	JSON(w, http.StatusOK, reaction)
}

// Remove handles DELETE /api/v1/posts/{postId}/reactions/{type}.
func (h *ReactionHandler) Remove(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	postID := chi.URLParam(r, "postId")
	reactionType := chi.URLParam(r, "type")
	if postID == "" || reactionType == "" {
		Error(w, http.StatusBadRequest, "missing post ID or reaction type")
		return
	}

	err := h.reactions.Remove(r.Context(), user.ID, postID, domain.ReactionType(reactionType))
	if err != nil {
		if errors.Is(err, service.ErrInvalidReactionType) {
			Error(w, http.StatusBadRequest, err.Error())
			return
		}
		Error(w, http.StatusInternalServerError, "failed to remove reaction")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

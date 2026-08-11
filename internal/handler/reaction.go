package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/service"
)

// ReactionEventPublisher publishes reaction events for SSE notifications.
//
// It is declared here, in terms the handler needs, rather than importing
// *sse.Broker: the handler is the consumer, so the dependency points inward and
// tests supply a stub without standing up a broker. (The comment this replaces
// said the sse package did not exist yet. It does — internal/sse — and
// server.routes wires the real broker in through WithReactionPublisher.)
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

	var req addReactionRequest
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	reaction, err := h.reactions.Add(r.Context(), user.ID, postID, domain.ReactionType(req.Type))
	if err != nil {
		serviceError(w, err)
		return
	}

	// Publish reaction event for SSE notification (if publisher is wired).
	// A notification that never arrives must not fail the reaction itself, so
	// both failures are logged rather than returned.
	if h.publisher != nil && h.posts != nil {
		post, err := h.posts.GetByID(r.Context(), postID)
		if err != nil {
			slog.Warn("loading post to notify its author of a reaction",
				"error", err, "post_id", postID, "reaction_id", reaction.ID)
		} else if err := h.publisher.PublishReactionEvent(r.Context(), postID, post.AuthorID, string(reaction.Type), user.ID); err != nil {
			slog.Warn("publishing reaction event",
				"error", err, "post_id", postID, "post_author_id", post.AuthorID, "reaction_id", reaction.ID)
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

	err := h.reactions.Remove(r.Context(), user.ID, postID, domain.ReactionType(reactionType))
	if err != nil {
		serviceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

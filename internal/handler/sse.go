package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/sse"
)

const sseHeartbeatInterval = 10 * time.Second

// shouldDeliver reports whether an event may be sent to the given viewer.
//
// New posts go to everyone. Reaction events are private to the post's author,
// since they say who reacted to whose post — so an undecodable reaction
// payload is dropped rather than delivered. Failing open here would broadcast
// every reaction to every connected client.
func shouldDeliver(evt sse.Event, viewerID string) bool {
	if evt.Type != sse.EventReaction {
		return true
	}

	var re sse.ReactionEvent
	if err := json.Unmarshal(evt.Data, &re); err != nil {
		return false
	}
	return re.PostAuthorID == viewerID
}

// EventSubscriber hands out a stream of events for one connected client.
// ServeFeed depends on this rather than on *sse.Broker so the streaming loop
// can be driven directly in tests; *sse.Broker satisfies it as-is.
type EventSubscriber interface {
	Subscribe(ctx context.Context) (<-chan sse.Event, error)
}

// SSEHandler serves Server-Sent Events for real-time feed updates.
type SSEHandler struct {
	broker EventSubscriber
	// heartbeatInterval is how often a comment frame is written to keep the
	// connection alive; tests shorten it so they need not wait for the real one.
	heartbeatInterval time.Duration
}

// NewSSEHandler creates a new SSEHandler backed by the given broker.
func NewSSEHandler(broker EventSubscriber) *SSEHandler {
	return &SSEHandler{broker: broker, heartbeatInterval: sseHeartbeatInterval}
}

// extendDeadline pushes the connection's write deadline past the next
// heartbeat. The server's WriteTimeout (15s) would otherwise kill this
// long-lived connection before the next frame is due.
func (h *SSEHandler) extendDeadline(rc *http.ResponseController) {
	_ = rc.SetWriteDeadline(time.Now().Add(h.heartbeatInterval + 10*time.Second))
}

// ServeFeed streams SSE events to an authenticated client.
func (h *SSEHandler) ServeFeed(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		Error(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Subscribe before committing to a stream: once the SSE headers are
	// flushed the status is 200 and an error response can no longer be sent.
	events, err := h.broker.Subscribe(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to subscribe")
		return
	}

	rc := http.NewResponseController(w)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()

	// Extend write deadline immediately — the default WriteTimeout (15s) would
	// kill this long-lived connection before the first heartbeat fires.
	h.extendDeadline(rc)

	heartbeat := time.NewTicker(h.heartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			h.extendDeadline(rc)

			if !shouldDeliver(evt, user.ID) {
				continue
			}

			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, evt.Data)
			flusher.Flush()
		case <-heartbeat.C:
			h.extendDeadline(rc)
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

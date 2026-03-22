package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/sse"
)

const sseHeartbeatInterval = 10 * time.Second

// SSEHandler serves Server-Sent Events for real-time feed updates.
type SSEHandler struct {
	broker *sse.Broker
}

// NewSSEHandler creates a new SSEHandler backed by the given broker.
func NewSSEHandler(broker *sse.Broker) *SSEHandler {
	return &SSEHandler{broker: broker}
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

	rc := http.NewResponseController(w)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()

	// Extend write deadline immediately — the default WriteTimeout (15s) would
	// kill this long-lived connection before the first heartbeat fires.
	_ = rc.SetWriteDeadline(time.Now().Add(sseHeartbeatInterval + 10*time.Second))

	events, err := h.broker.Subscribe(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to subscribe")
		return
	}

	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			_ = rc.SetWriteDeadline(time.Now().Add(sseHeartbeatInterval + 10*time.Second))

			// For reaction events, only send to the post author.
			if evt.Type == sse.EventReaction {
				var re sse.ReactionEvent
				if err := json.Unmarshal(evt.Data, &re); err == nil {
					if re.PostAuthorID != user.ID {
						continue
					}
				}
			}

			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, evt.Data)
			flusher.Flush()
		case <-heartbeat.C:
			_ = rc.SetWriteDeadline(time.Now().Add(sseHeartbeatInterval + 10*time.Second))
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

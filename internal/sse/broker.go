package sse

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/redis/go-redis/v9"
)

// EventType identifies the kind of SSE event.
type EventType string

const (
	EventNewPost  EventType = "new_post"
	EventReaction EventType = "reaction"
)

// Event is a single SSE event sent to clients.
type Event struct {
	Type EventType       `json:"type"`
	Data json.RawMessage `json:"data"`
}

// ReactionEvent carries details about a reaction for SSE delivery.
type ReactionEvent struct {
	PostID       string `json:"post_id"`
	PostAuthorID string `json:"post_author_id"`
	ReactionType string `json:"reaction_type"`
	ReactorID    string `json:"reactor_id"`
}

const (
	channelPosts     = "bell:posts:new"
	channelReactions = "bell:reactions:new"
)

// Broker manages Redis pub/sub for real-time SSE events.
type Broker struct {
	rdb    redis.Cmdable
	logger *slog.Logger
}

// NewBroker creates a Broker backed by the given Redis client.
func NewBroker(rdb redis.Cmdable, logger *slog.Logger) *Broker {
	return &Broker{rdb: rdb, logger: logger}
}

// PublishPost publishes a new-post event with the given JSON payload.
func (b *Broker) PublishPost(ctx context.Context, postJSON []byte) error {
	return b.rdb.Publish(ctx, channelPosts, postJSON).Err()
}

// PublishReaction publishes a reaction event.
func (b *Broker) PublishReaction(ctx context.Context, event ReactionEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return b.rdb.Publish(ctx, channelReactions, data).Err()
}

// PublishReactionEvent implements handler.ReactionEventPublisher interface.
func (b *Broker) PublishReactionEvent(ctx context.Context, postID, postAuthorID, reactionType, reactorID string) error {
	return b.PublishReaction(ctx, ReactionEvent{
		PostID:       postID,
		PostAuthorID: postAuthorID,
		ReactionType: reactionType,
		ReactorID:    reactorID,
	})
}

// Subscribe returns a channel that receives events from Redis pub/sub.
// Cancel the context to unsubscribe.
func (b *Broker) Subscribe(ctx context.Context) (<-chan Event, error) {
	client, ok := b.rdb.(*redis.Client)
	if !ok {
		// Fallback for testing with mocks — return a channel that blocks until context done.
		ch := make(chan Event)
		go func() { <-ctx.Done(); close(ch) }()
		return ch, nil
	}

	pubsub := client.Subscribe(ctx, channelPosts, channelReactions)
	events := make(chan Event, 16)

	go func() {
		defer close(events)
		defer pubsub.Close()
		ch := pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var evt Event
				switch msg.Channel {
				case channelPosts:
					evt = Event{Type: EventNewPost, Data: json.RawMessage(msg.Payload)}
				case channelReactions:
					evt = Event{Type: EventReaction, Data: json.RawMessage(msg.Payload)}
				}
				select {
				case events <- evt:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return events, nil
}

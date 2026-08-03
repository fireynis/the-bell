package sse

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestEventFromMessage(t *testing.T) {
	tests := []struct {
		name      string
		channel   string
		payload   string
		wantType  EventType
		wantKnown bool
	}{
		{"post channel", channelPosts, `{"id":"p1"}`, EventNewPost, true},
		{"reaction channel", channelReactions, `{"post_id":"p1"}`, EventReaction, true},
		{"unknown channel is dropped", "bell:something:else", `{}`, "", false},
		{"empty channel is dropped", "", `{}`, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evt, known := eventFromMessage(tt.channel, tt.payload)
			if known != tt.wantKnown {
				t.Fatalf("known = %v, want %v", known, tt.wantKnown)
			}
			if !known {
				return
			}
			if evt.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", evt.Type, tt.wantType)
			}
			if string(evt.Data) != tt.payload {
				t.Errorf("Data = %q, want the payload verbatim %q", evt.Data, tt.payload)
			}
		})
	}
}

// A message from a channel this broker does not own must never reach
// subscribers as an event with an empty type, which clients cannot dispatch on.
func TestEventFromMessage_NeverReturnsUntypedEvent(t *testing.T) {
	for _, channel := range []string{"", "other", "bell:posts:new:extra", "BELL:POSTS:NEW"} {
		evt, known := eventFromMessage(channel, `{"a":1}`)
		if known && evt.Type == "" {
			t.Errorf("channel %q reported a known event with an empty type", channel)
		}
	}
}

// The payload is forwarded without re-encoding, so a client receives exactly
// the JSON the publisher wrote.
func TestEventFromMessage_PayloadPassedThroughUnmodified(t *testing.T) {
	payload := `{"id":"p1","body":"hello \"world\"","nested":{"a":[1,2,3]}}`

	evt, known := eventFromMessage(channelPosts, payload)
	if !known {
		t.Fatal("expected the post channel to be known")
	}
	if !json.Valid(evt.Data) {
		t.Fatalf("Data is not valid JSON: %s", evt.Data)
	}
	if string(evt.Data) != payload {
		t.Errorf("Data = %s, want %s", evt.Data, payload)
	}
}

// recordingPublisher captures Publish calls without needing a Redis server.
type recordingPublisher struct {
	redis.Cmdable
	channel string
	payload string
	err     error
}

func (p *recordingPublisher) Publish(ctx context.Context, channel string, message any) *redis.IntCmd {
	p.channel = channel
	if b, ok := message.([]byte); ok {
		p.payload = string(b)
	}
	cmd := redis.NewIntCmd(ctx)
	if p.err != nil {
		cmd.SetErr(p.err)
	}
	return cmd
}

func TestBroker_PublishPost(t *testing.T) {
	pub := &recordingPublisher{}
	b := NewBroker(pub, discardLogger())

	body := []byte(`{"id":"p1"}`)
	if err := b.PublishPost(context.Background(), body); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pub.channel != channelPosts {
		t.Errorf("channel = %q, want %q", pub.channel, channelPosts)
	}
	if pub.payload != string(body) {
		t.Errorf("payload = %q, want %q", pub.payload, body)
	}
}

func TestBroker_PublishPost_PropagatesError(t *testing.T) {
	wantErr := errors.New("redis down")
	b := NewBroker(&recordingPublisher{err: wantErr}, discardLogger())

	if err := b.PublishPost(context.Background(), []byte(`{}`)); !errors.Is(err, wantErr) {
		t.Errorf("error = %v, want %v", err, wantErr)
	}
}

func TestBroker_PublishReaction(t *testing.T) {
	pub := &recordingPublisher{}
	b := NewBroker(pub, discardLogger())

	event := ReactionEvent{
		PostID:       "p1",
		PostAuthorID: "author",
		ReactionType: "bell",
		ReactorID:    "reactor",
	}
	if err := b.PublishReaction(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pub.channel != channelReactions {
		t.Errorf("channel = %q, want %q", pub.channel, channelReactions)
	}

	var got ReactionEvent
	if err := json.Unmarshal([]byte(pub.payload), &got); err != nil {
		t.Fatalf("payload is not valid ReactionEvent JSON: %v", err)
	}
	if got != event {
		t.Errorf("round-tripped event = %+v, want %+v", got, event)
	}
}

// PublishReactionEvent is the handler-facing wrapper; it must produce the same
// payload as calling PublishReaction directly.
func TestBroker_PublishReactionEvent_MatchesPublishReaction(t *testing.T) {
	viaWrapper := &recordingPublisher{}
	if err := NewBroker(viaWrapper, discardLogger()).
		PublishReactionEvent(context.Background(), "p1", "author", "bell", "reactor"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	viaDirect := &recordingPublisher{}
	if err := NewBroker(viaDirect, discardLogger()).PublishReaction(context.Background(), ReactionEvent{
		PostID: "p1", PostAuthorID: "author", ReactionType: "bell", ReactorID: "reactor",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if viaWrapper.payload != viaDirect.payload {
		t.Errorf("wrapper payload %q differs from direct payload %q", viaWrapper.payload, viaDirect.payload)
	}
	if viaWrapper.channel != viaDirect.channel {
		t.Errorf("wrapper channel %q differs from direct channel %q", viaWrapper.channel, viaDirect.channel)
	}
}

// Subscribe falls back to a context-bound channel when the client is not a real
// *redis.Client. Cancelling the context must close that channel so callers
// ranging over it terminate instead of leaking a goroutine.
func TestBroker_Subscribe_ClosesOnContextCancel(t *testing.T) {
	b := NewBroker(&recordingPublisher{}, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	events, err := b.Subscribe(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cancel()

	select {
	case _, open := <-events:
		if open {
			t.Error("expected the channel to be closed after cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("channel was not closed within 2s of cancelling the context")
	}
}

func TestBroker_Subscribe_BlocksUntilCancelled(t *testing.T) {
	b := NewBroker(&recordingPublisher{}, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := b.Subscribe(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-events:
		t.Error("received an event before anything was published")
	case <-time.After(50 * time.Millisecond):
		// Expected: nothing arrives and the channel stays open.
	}
}

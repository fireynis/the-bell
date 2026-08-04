package sse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/testsupport"
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

// A client that cannot open a pub/sub connection must fail loudly. Handing back
// a channel that never delivers would leave every client's live feed silently
// and permanently empty, with nothing logged and nothing failing.
func TestBroker_Subscribe_RejectsClientsThatCannotSubscribe(t *testing.T) {
	b := NewBroker(&recordingPublisher{}, discardLogger())

	events, err := b.Subscribe(context.Background())

	if !errors.Is(err, ErrSubscribeUnsupported) {
		t.Errorf("error = %v, want %v", err, ErrSubscribeUnsupported)
	}
	if events != nil {
		t.Error("a channel was returned alongside the error; callers may range over it forever")
	}
}

// The type of the unusable client belongs in the log — it is the only clue to
// which wiring produced it.
func TestBroker_Subscribe_LogsTheUnusableClientType(t *testing.T) {
	var logged bytes.Buffer
	b := NewBroker(&recordingPublisher{}, slog.New(slog.NewTextHandler(&logged, nil)))

	if _, err := b.Subscribe(context.Background()); err == nil {
		t.Fatal("expected an error")
	}

	if !strings.Contains(logged.String(), "recordingPublisher") {
		t.Errorf("log %q does not name the offending client type", logged.String())
	}
}

// newRedisBroker returns a broker backed by a real *redis.Client talking to a
// real Redis. This is the only path that can subscribe at all.
//
// These tests previously ran against miniredis. Pub/sub is where an in-process
// reimplementation is least trustworthy — subscriber registration timing,
// connection release on cancel and buffer-overflow behaviour are precisely what
// is asserted here, and asserting them against a reimplementation proved
// nothing about the Redis production talks to.
func newRedisBroker(t *testing.T) (*Broker, *redis.Client) {
	t.Helper()

	rdb := testsupport.TestRedis(t)

	// Pub/sub is global to a Redis server rather than scoped to a logical
	// database, so a subscription a previous test has not finished releasing
	// would be counted here. Start from a known-empty state.
	waitForSubscriberCount(t, rdb, 0)

	return NewBroker(rdb, discardLogger()), rdb
}

// subscriberCount reports how many subscriptions to the posts channel Redis
// currently holds, via PUBSUB NUMSUB.
func subscriberCount(t *testing.T, rdb *redis.Client) int {
	t.Helper()

	counts, err := rdb.PubSubNumSub(context.Background(), channelPosts).Result()
	if err != nil {
		t.Fatalf("PUBSUB NUMSUB: %v", err)
	}
	return int(counts[channelPosts])
}

// waitForSubscriberCount blocks until exactly n subscriptions to the posts
// channel are registered with Redis.
//
// The count is matched exactly rather than as a floor: the tests below wait for
// subscribers to appear *and* for cancelled ones to go away, and a >= check
// would return immediately in the second case, letting the test proceed while
// the subscriptions it expects to be gone are still live.
func waitForSubscriberCount(t *testing.T, rdb *redis.Client, n int) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	var got int
	for time.Now().Before(deadline) {
		got = subscriberCount(t, rdb)
		if got == n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("%d subscribers registered within 5s, want exactly %d", got, n)
}

// waitForSubscribers blocks until n subscriptions to the posts channel have
// reached Redis, so a publish cannot race ahead of the subscribe.
func waitForSubscribers(t *testing.T, rdb *redis.Client, n int) {
	t.Helper()
	waitForSubscriberCount(t, rdb, n)
}

// publish sends a message on a channel and fails the test if Redis rejects it.
func publish(t *testing.T, rdb *redis.Client, channel, payload string) {
	t.Helper()

	if err := rdb.Publish(context.Background(), channel, payload).Err(); err != nil {
		t.Fatalf("publishing to %s: %v", channel, err)
	}
}

// waitForBufferFull blocks until the subscriber channel holds every event it
// can, so a test can rely on the pub/sub goroutine being blocked on its send.
func waitForBufferFull(t *testing.T, events <-chan Event) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(events) == cap(events) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("buffer held %d of %d events within 2s", len(events), cap(events))
}

// waitForNoSubscribers blocks until the broker's subscription has been released
// at the Redis end, which only happens once the pub/sub goroutine has returned
// and run its deferred pubsub.Close.
//
// This asserts the connection is released, not merely that the event channel
// closed — the two are separate, and only the former proves the goroutine and
// its Redis connection are not accumulating per disconnected client.
func waitForNoSubscribers(t *testing.T, rdb *redis.Client) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if subscriberCount(t, rdb) == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the subscription was never released; the pub/sub goroutine is still running")
}

// receive returns the next event, failing rather than blocking if none arrives.
func receive(t *testing.T, events <-chan Event) Event {
	t.Helper()

	select {
	case evt, ok := <-events:
		if !ok {
			t.Fatal("the event channel closed before an event arrived")
		}
		return evt
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for an event")
		return Event{}
	}
}

func TestBroker_Subscribe_DeliversPublishedMessages(t *testing.T) {
	tests := []struct {
		name     string
		channel  string
		payload  string
		wantType EventType
	}{
		{"new post", channelPosts, `{"id":"p1"}`, EventNewPost},
		{"reaction", channelReactions, `{"post_id":"p1"}`, EventReaction},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, rdb := newRedisBroker(t)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			events, err := b.Subscribe(ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			waitForSubscribers(t, rdb, 1)

			publish(t, rdb, tt.channel, tt.payload)

			evt := receive(t, events)
			if evt.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", evt.Type, tt.wantType)
			}
			if string(evt.Data) != tt.payload {
				t.Errorf("Data = %q, want %q", evt.Data, tt.payload)
			}
		})
	}
}

// Every connected SSE client holds one subscription. Cancelling the request
// context must close that client's channel so the pub/sub goroutine and its
// Redis connection are released instead of accumulating.
func TestBroker_Subscribe_RedisPathClosesOnContextCancel(t *testing.T) {
	b, rdb := newRedisBroker(t)

	ctx, cancel := context.WithCancel(context.Background())
	events, err := b.Subscribe(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	waitForSubscribers(t, rdb, 1)

	cancel()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, open := <-events:
			if !open {
				return // closed, as required
			}
		case <-deadline:
			t.Fatal("the event channel was not closed within 2s of cancelling the context")
		}
	}
}

// A client that stops reading (a stalled browser, a wedged connection) fills
// the 16-slot buffer. That must never block the publisher or wedge the pub/sub
// goroutine: cancelling still tears the subscription down cleanly.
func TestBroker_Subscribe_SlowSubscriberDoesNotBlockThePublisher(t *testing.T) {
	b, rdb := newRedisBroker(t)

	ctx, cancel := context.WithCancel(context.Background())
	events, err := b.Subscribe(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	waitForSubscribers(t, rdb, 1)

	// One more than the buffer holds, with nothing draining it. Publishing must
	// not block even though the subscriber has stopped reading.
	published := make(chan struct{})
	go func() {
		defer close(published)
		for i := 0; i <= subscriberBuffer; i++ {
			publish(t, rdb, channelPosts, `{"id":"p1"}`)
		}
	}()

	select {
	case <-published:
	case <-time.After(2 * time.Second):
		t.Fatal("publishing blocked on a subscriber that stopped reading")
	}

	// Wait for the buffer to fill. Once it holds all it can and one message is
	// still in flight, the goroutine can only be blocked handing that message
	// over — which is the state cancellation has to rescue it from.
	waitForBufferFull(t, events)

	cancel()

	// Wait for the subscription to disappear from Redis *before* reading
	// anything. Draining first would let the goroutine escape by completing its
	// blocked send; with no reader, cancellation is the only way out, which is
	// the path that matters for a wedged client.
	waitForNoSubscribers(t, rdb)

	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, open := <-events:
			if !open {
				return // drained and closed, with no double-close panic
			}
		case <-deadline:
			t.Fatal("a stalled subscriber's channel was never closed after cancellation")
		}
	}
}

// Each SSE client subscribes independently, so one client's disconnect must not
// disturb the others' streams.
func TestBroker_Subscribe_ConcurrentSubscribersAreIndependent(t *testing.T) {
	const subscribers = 8

	b, rdb := newRedisBroker(t)

	ctx, cancelAll := context.WithCancel(context.Background())
	defer cancelAll()

	streams := make([]<-chan Event, subscribers)
	cancels := make([]context.CancelFunc, subscribers)
	for i := range streams {
		subCtx, cancel := context.WithCancel(ctx)
		cancels[i] = cancel

		events, err := b.Subscribe(subCtx)
		if err != nil {
			t.Fatalf("subscriber %d: unexpected error: %v", i, err)
		}
		streams[i] = events
	}
	waitForSubscribers(t, rdb, subscribers)

	// Drop the odd-numbered subscribers while the rest keep streaming.
	for i := 1; i < subscribers; i += 2 {
		cancels[i]()
	}
	waitForSubscribers(t, rdb, subscribers/2)

	publish(t, rdb, channelPosts, `{"id":"p1"}`)

	for i := 0; i < subscribers; i += 2 {
		if evt := receive(t, streams[i]); evt.Type != EventNewPost {
			t.Errorf("subscriber %d received %q, want %q", i, evt.Type, EventNewPost)
		}
	}
}

// An idle subscription must stay open and empty — no spurious events from the
// pub/sub plumbing itself, which clients would render as phantom posts.
func TestBroker_Subscribe_IdleStreamDeliversNothing(t *testing.T) {
	b, rdb := newRedisBroker(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := b.Subscribe(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	waitForSubscribers(t, rdb, 1)

	select {
	case evt, open := <-events:
		if !open {
			t.Error("the channel closed while the context was still live")
		} else {
			t.Errorf("received event %+v before anything was published", evt)
		}
	case <-time.After(50 * time.Millisecond):
		// Expected: nothing arrives and the channel stays open.
	}
}

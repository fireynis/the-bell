package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/sse"
)

// The real broker must keep satisfying EventSubscriber, so NewSSEHandler stays
// source-compatible with its call site in internal/server/routes.go.
var _ EventSubscriber = (*sse.Broker)(nil)

// testTimeout bounds every wait in this file: a regression that stops writing
// frames or stops returning fails the test quickly instead of hanging CI.
const testTimeout = 2 * time.Second

// fakeSubscriber stands in for the Redis-backed broker. The broker is
// in-process, so a fake is enough to drive every branch of the streaming loop.
type fakeSubscriber struct {
	events chan sse.Event
	err    error
}

func newFakeSubscriber() *fakeSubscriber {
	return &fakeSubscriber{events: make(chan sse.Event, 8)}
}

func (f *fakeSubscriber) Subscribe(context.Context) (<-chan sse.Event, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.events, nil
}

// feed is a live SSE connection to a ServeFeed running on a real HTTP server,
// so http.ResponseController and Flush behave as they do in production.
type feed struct {
	resp *http.Response
	// lines carries one response line at a time, closed when the body ends.
	lines <-chan string
	// served is closed when ServeFeed returns.
	served <-chan struct{}
	// disconnect drops the client connection, as a closed browser tab would.
	disconnect context.CancelFunc
}

func startFeed(t *testing.T, sub EventSubscriber, viewerID string) *feed {
	t.Helper()

	h := NewSSEHandler(sub)
	// Short enough that a heartbeat arrives within the test's timeout.
	h.heartbeatInterval = 20 * time.Millisecond

	served := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(served)
		ctx := middleware.WithUser(r.Context(), &domain.User{ID: viewerID})
		h.ServeFeed(w, r.WithContext(ctx))
	}))

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		cancel()
		srv.Close()
		t.Fatalf("building request: %v", err)
	}

	resp, err := srv.Client().Do(req)
	if err != nil {
		cancel()
		srv.Close()
		t.Fatalf("connecting to the feed: %v", err)
	}

	t.Cleanup(func() {
		cancel()
		resp.Body.Close()
		select {
		case <-served:
		case <-time.After(testTimeout):
			t.Error("ServeFeed did not return after the client disconnected")
		}
		srv.Close()
	})

	return &feed{resp: resp, lines: readLines(t, resp.Body), served: served, disconnect: cancel}
}

// readLines pumps the response body onto a channel so a test can wait for a
// frame with a bounded select instead of blocking on a read that may never
// return.
func readLines(t *testing.T, body io.Reader) <-chan string {
	t.Helper()

	done := make(chan struct{})
	t.Cleanup(func() { close(done) })

	lines := make(chan string)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(body)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-done:
				return
			}
		}
	}()
	return lines
}

// nextLine returns the next line written to the stream, failing the test rather
// than blocking if none arrives.
func (f *feed) nextLine(t *testing.T) string {
	t.Helper()

	select {
	case line, ok := <-f.lines:
		if !ok {
			t.Fatal("the stream closed before the expected line arrived")
		}
		return line
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for a line from the SSE stream")
		return ""
	}
}

func TestNewSSEHandler_UsesTheProductionHeartbeatByDefault(t *testing.T) {
	h := NewSSEHandler(newFakeSubscriber())

	if h.heartbeatInterval != sseHeartbeatInterval {
		t.Errorf("heartbeatInterval = %v, want %v", h.heartbeatInterval, sseHeartbeatInterval)
	}
}

func TestServeFeed_RejectsUnauthenticatedRequests(t *testing.T) {
	h := NewSSEHandler(newFakeSubscriber())

	rec := httptest.NewRecorder()
	h.ServeFeed(rec, httptest.NewRequest(http.MethodGet, "/api/v1/feed/stream", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if ct := rec.Header().Get("Content-Type"); ct == "text/event-stream" {
		t.Error("an unauthenticated request was answered with a stream")
	}
}

// nonFlushingWriter is a ResponseWriter that cannot flush, which is the one
// case a real server never produces.
type nonFlushingWriter struct {
	header http.Header
	status int
	body   strings.Builder
}

func (w *nonFlushingWriter) Header() http.Header         { return w.header }
func (w *nonFlushingWriter) Write(b []byte) (int, error) { return w.body.Write(b) }
func (w *nonFlushingWriter) WriteHeader(code int)        { w.status = code }

func TestServeFeed_RefusesWritersThatCannotFlush(t *testing.T) {
	h := NewSSEHandler(newFakeSubscriber())

	w := &nonFlushingWriter{header: http.Header{}, status: http.StatusOK}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/feed/stream", nil)
	req = req.WithContext(middleware.WithUser(req.Context(), &domain.User{ID: "viewer"}))

	h.ServeFeed(w, req)

	if w.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.status, http.StatusInternalServerError)
	}
}

// A failed subscription must surface as a plain 500. The SSE headers commit the
// response to 200 and to a stream body, so they must not be written until the
// subscription succeeds.
func TestServeFeed_SubscribeFailureReturns500WithoutStreamHeaders(t *testing.T) {
	sub := newFakeSubscriber()
	sub.err = errors.New("redis down")

	f := startFeed(t, sub, "viewer")

	if f.resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", f.resp.StatusCode, http.StatusInternalServerError)
	}
	if ct := f.resp.Header.Get("Content-Type"); ct == "text/event-stream" {
		t.Errorf("Content-Type = %q, want a non-stream type", ct)
	}
	if cc := f.resp.Header.Get("Cache-Control"); cc != "" {
		t.Errorf("Cache-Control = %q, want it unset on the error response", cc)
	}
}

func TestServeFeed_SendsStreamHeaders(t *testing.T) {
	f := startFeed(t, newFakeSubscriber(), "viewer")

	if f.resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", f.resp.StatusCode, http.StatusOK)
	}

	// X-Accel-Buffering stops nginx from buffering the stream into silence.
	want := map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-cache",
		"Connection":        "keep-alive",
		"X-Accel-Buffering": "no",
	}
	for header, value := range want {
		if got := f.resp.Header.Get(header); got != value {
			t.Errorf("%s = %q, want %q", header, got, value)
		}
	}
}

// A delivered event must be a well-formed SSE frame: an event line, a data
// line, and the blank line that terminates the frame.
func TestServeFeed_WritesDeliverableEventsInSSEWireFormat(t *testing.T) {
	sub := newFakeSubscriber()
	f := startFeed(t, sub, "viewer")

	payload := `{"id":"p1","body":"hello"}`
	sub.events <- sse.Event{Type: sse.EventNewPost, Data: json.RawMessage(payload)}

	if got, want := f.nextLine(t), "event: new_post"; got != want {
		t.Errorf("first line = %q, want %q", got, want)
	}
	if got, want := f.nextLine(t), "data: "+payload; got != want {
		t.Errorf("second line = %q, want %q", got, want)
	}
	if got := f.nextLine(t); got != "" {
		t.Errorf("frame terminator = %q, want an empty line", got)
	}
}

// The leak fixed in 3e0014f: reaction events name who reacted to whose post, so
// a viewer must only ever see reactions on their own posts. The trailing new
// post proves the withheld reaction was skipped rather than merely delayed.
func TestServeFeed_WithholdsReactionsOnOtherPeoplesPosts(t *testing.T) {
	sub := newFakeSubscriber()
	f := startFeed(t, sub, "viewer")

	sub.events <- reactionEvent(t, "someone-else")
	sub.events <- sse.Event{Type: sse.EventNewPost, Data: json.RawMessage(`{"id":"p2"}`)}

	if got, want := f.nextLine(t), "event: new_post"; got != want {
		t.Errorf("first delivered line = %q, want %q — a reaction on another user's post leaked", got, want)
	}
}

func TestServeFeed_DeliversReactionsOnTheViewersOwnPosts(t *testing.T) {
	sub := newFakeSubscriber()
	f := startFeed(t, sub, "viewer")

	sub.events <- reactionEvent(t, "viewer")

	if got, want := f.nextLine(t), "event: reaction"; got != want {
		t.Errorf("first delivered line = %q, want %q", got, want)
	}
}

// Heartbeats keep idle connections (and the proxies in front of them) alive.
func TestServeFeed_EmitsHeartbeatsWhileIdle(t *testing.T) {
	f := startFeed(t, newFakeSubscriber(), "viewer")

	if got, want := f.nextLine(t), ": heartbeat"; got != want {
		t.Errorf("first line on an idle stream = %q, want %q", got, want)
	}
	if got := f.nextLine(t); got != "" {
		t.Errorf("heartbeat terminator = %q, want an empty line", got)
	}
}

// A closed tab must release the handler goroutine; this is the app's only
// long-lived connection, so a leak here accumulates.
func TestServeFeed_ReturnsWhenTheClientDisconnects(t *testing.T) {
	f := startFeed(t, newFakeSubscriber(), "viewer")

	// Wait for a heartbeat so the handler is known to be inside the loop.
	f.nextLine(t)
	f.disconnect()

	select {
	case <-f.served:
	case <-time.After(testTimeout):
		t.Fatal("ServeFeed did not return after the client disconnected")
	}
}

// The broker closes its channel when its context ends; the handler must return
// instead of spinning on a closed channel.
func TestServeFeed_ReturnsWhenTheBrokerClosesTheStream(t *testing.T) {
	sub := newFakeSubscriber()
	f := startFeed(t, sub, "viewer")

	close(sub.events)

	select {
	case <-f.served:
	case <-time.After(testTimeout):
		t.Fatal("ServeFeed did not return after the broker closed the event channel")
	}
}

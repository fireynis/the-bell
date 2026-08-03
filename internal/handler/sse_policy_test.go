package handler

import (
	"encoding/json"
	"testing"

	"github.com/fireynis/the-bell/internal/sse"
)

func reactionEvent(t *testing.T, authorID string) sse.Event {
	t.Helper()
	data, err := json.Marshal(sse.ReactionEvent{
		PostID:       "p1",
		PostAuthorID: authorID,
		ReactionType: "bell",
		ReactorID:    "someone",
	})
	if err != nil {
		t.Fatalf("marshaling reaction event: %v", err)
	}
	return sse.Event{Type: sse.EventReaction, Data: data}
}

func TestShouldDeliver_NewPostsGoToEveryone(t *testing.T) {
	evt := sse.Event{Type: sse.EventNewPost, Data: json.RawMessage(`{"id":"p1"}`)}

	for _, viewer := range []string{"author", "stranger", ""} {
		if !shouldDeliver(evt, viewer) {
			t.Errorf("new post withheld from viewer %q", viewer)
		}
	}
}

func TestShouldDeliver_ReactionsOnlyReachThePostAuthor(t *testing.T) {
	evt := reactionEvent(t, "author")

	if !shouldDeliver(evt, "author") {
		t.Error("reaction was withheld from the post author")
	}
	if shouldDeliver(evt, "stranger") {
		t.Error("reaction on another user's post was delivered to a stranger")
	}
}

// A reaction payload that cannot be decoded must be dropped. Failing open would
// broadcast who reacted to whose post to every connected client.
func TestShouldDeliver_MalformedReactionFailsClosed(t *testing.T) {
	malformed := []json.RawMessage{
		json.RawMessage(`{not json`),
		json.RawMessage(``),
		json.RawMessage(`null`),
		json.RawMessage(`"a string"`),
		json.RawMessage(`[1,2,3]`),
	}

	for _, data := range malformed {
		evt := sse.Event{Type: sse.EventReaction, Data: data}
		if shouldDeliver(evt, "stranger") {
			t.Errorf("undecodable reaction payload %q was delivered", data)
		}
	}
}

// An empty author ID must not match an empty viewer ID by accident.
func TestShouldDeliver_EmptyAuthorDoesNotMatchEveryone(t *testing.T) {
	evt := reactionEvent(t, "author")

	if shouldDeliver(evt, "") {
		t.Error("reaction delivered to a viewer with no ID")
	}
}

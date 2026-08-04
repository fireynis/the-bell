//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The batch reaction queries pass an array parameter and group in SQL, which is
// exactly where a wrong join or a wrong GROUP BY still returns plausible-looking
// numbers. These run against a real database and assert attribution — which
// post, which user — not just totals.

var reactionSeq int

func newReaction(t *testing.T, pool *pgxpool.Pool, userID, postID string, rt domain.ReactionType) *domain.Reaction {
	t.Helper()

	reactionSeq++
	reaction := &domain.Reaction{
		ID:        fmt.Sprintf("reaction-%d", reactionSeq),
		UserID:    userID,
		PostID:    postID,
		Type:      rt,
		CreatedAt: time.Now(),
	}
	if err := postgres.NewReactionRepo(postgres.New(pool)).AddReaction(context.Background(), reaction); err != nil {
		t.Fatalf("adding reaction %s: %v", reaction.ID, err)
	}
	return reaction
}

// reactionFixture builds a deliberately lopsided arrangement: every post has a
// different total and every user a different set, so any mis-attribution
// changes an assertion rather than shuffling equal numbers around.
type reactionFixture struct {
	pool                *pgxpool.Pool
	repo                *postgres.ReactionRepo
	author              *domain.User
	u1, u2, u3          *domain.User
	postA, postB, postC string
}

func newReactionFixture(t *testing.T) reactionFixture {
	t.Helper()

	pool := testsupport.TestDB(t)
	f := reactionFixture{
		pool:  pool,
		repo:  postgres.NewReactionRepo(postgres.New(pool)),
		postA: "react-post-a",
		postB: "react-post-b",
		postC: "react-post-c",
	}

	f.author = testsupport.TestUser(t, pool, testsupport.UniqueKratosID("react-author"), domain.RoleMember, 50)
	f.u1 = testsupport.TestUser(t, pool, testsupport.UniqueKratosID("react-u1"), domain.RoleMember, 50)
	f.u2 = testsupport.TestUser(t, pool, testsupport.UniqueKratosID("react-u2"), domain.RoleMember, 50)
	f.u3 = testsupport.TestUser(t, pool, testsupport.UniqueKratosID("react-u3"), domain.RoleMember, 50)

	now := time.Now()
	for _, id := range []string{f.postA, f.postB, f.postC} {
		newPost(t, pool, id, f.author.ID, domain.PostVisible, now)
	}

	// postA: 3 bells and 1 heart. postB: 1 bell. postC: nothing at all.
	newReaction(t, pool, f.u1.ID, f.postA, domain.ReactionBell)
	newReaction(t, pool, f.u2.ID, f.postA, domain.ReactionBell)
	newReaction(t, pool, f.u3.ID, f.postA, domain.ReactionBell)
	newReaction(t, pool, f.u1.ID, f.postA, domain.ReactionHeart)
	newReaction(t, pool, f.u2.ID, f.postB, domain.ReactionBell)

	return f
}

// --- BatchCountByPosts ---

func TestReactionRepo_BatchCountByPosts_AttributesCountsToTheRightPost(t *testing.T) {
	f := newReactionFixture(t)

	got, err := f.repo.BatchCountByPosts(context.Background(), []string{f.postA, f.postB, f.postC})
	if err != nil {
		t.Fatalf("BatchCountByPosts: %v", err)
	}

	want := map[string]map[domain.ReactionType]int{
		f.postA: {domain.ReactionBell: 3, domain.ReactionHeart: 1},
		f.postB: {domain.ReactionBell: 1},
	}
	for postID, wantCounts := range want {
		gotCounts := got[postID]
		if len(gotCounts) != len(wantCounts) {
			t.Errorf("post %s: got %v, want %v", postID, gotCounts, wantCounts)
			continue
		}
		for rt, n := range wantCounts {
			if gotCounts[rt] != n {
				t.Errorf("post %s %s = %d, want %d", postID, rt, gotCounts[rt], n)
			}
		}
	}

	// A post with no reactions gets no entry at all rather than a zero map, so
	// the handler's `if c, ok := counts[p.ID]` leaves its post untouched.
	if _, present := got[f.postC]; present {
		t.Errorf("post with no reactions has entry %v, want it absent", got[f.postC])
	}
	if len(got) != 2 {
		t.Errorf("result has %d posts, want only the 2 with reactions", len(got))
	}
}

// Only the requested posts come back, even though other posts in the table have
// reactions — a missing WHERE would silently return the whole table.
func TestReactionRepo_BatchCountByPosts_IgnoresPostsNotAskedFor(t *testing.T) {
	f := newReactionFixture(t)

	got, err := f.repo.BatchCountByPosts(context.Background(), []string{f.postB})
	if err != nil {
		t.Fatalf("BatchCountByPosts: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("got %d posts (%v), want only %s", len(got), got, f.postB)
	}
	if got[f.postB][domain.ReactionBell] != 1 {
		t.Errorf("post %s bell = %d, want 1", f.postB, got[f.postB][domain.ReactionBell])
	}
}

func TestReactionRepo_BatchCountByPosts_EmptyAndUnknownInput(t *testing.T) {
	f := newReactionFixture(t)

	tests := []struct {
		name    string
		postIDs []string
	}{
		{"nil slice", nil},
		{"empty slice", []string{}},
		{"unknown post ids", []string{"no-such-post", "also-missing"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.repo.BatchCountByPosts(context.Background(), tt.postIDs)
			if err != nil {
				t.Fatalf("BatchCountByPosts: %v", err)
			}
			if len(got) != 0 {
				t.Errorf("got %v, want an empty result", got)
			}
		})
	}
}

// --- BatchGetUserReactions ---

func TestReactionRepo_BatchGetUserReactions_DoesNotLeakAcrossUsers(t *testing.T) {
	f := newReactionFixture(t)
	ctx := context.Background()
	posts := []string{f.postA, f.postB, f.postC}

	// u1 reacted twice to postA and never to postB.
	got, err := f.repo.BatchGetUserReactions(ctx, f.u1.ID, posts)
	if err != nil {
		t.Fatalf("BatchGetUserReactions: %v", err)
	}
	if types := sortedTypes(got[f.postA]); len(types) != 2 || types[0] != string(domain.ReactionBell) || types[1] != string(domain.ReactionHeart) {
		t.Errorf("u1 postA = %v, want [bell heart]", types)
	}
	if _, present := got[f.postB]; present {
		t.Errorf("u1 has an entry for postB (%v), but only u2 reacted there", got[f.postB])
	}

	// u2 reacted once to each of postA and postB — a query keyed on the wrong
	// column would hand u2 the same rows it handed u1.
	got, err = f.repo.BatchGetUserReactions(ctx, f.u2.ID, posts)
	if err != nil {
		t.Fatalf("BatchGetUserReactions: %v", err)
	}
	if len(got[f.postA]) != 1 || got[f.postA][0] != domain.ReactionBell {
		t.Errorf("u2 postA = %v, want [bell]", got[f.postA])
	}
	if len(got[f.postB]) != 1 || got[f.postB][0] != domain.ReactionBell {
		t.Errorf("u2 postB = %v, want [bell]", got[f.postB])
	}

	// A user who reacted to nothing gets nothing, not everyone else's rows.
	bystander := testsupport.TestUser(t, f.pool, testsupport.UniqueKratosID("react-bystander"), domain.RoleMember, 50)
	got, err = f.repo.BatchGetUserReactions(ctx, bystander.ID, posts)
	if err != nil {
		t.Fatalf("BatchGetUserReactions: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("bystander got %v, want nothing", got)
	}
}

func TestReactionRepo_BatchGetUserReactions_EmptyAndUnknownInput(t *testing.T) {
	f := newReactionFixture(t)

	tests := []struct {
		name    string
		userID  string
		postIDs []string
	}{
		{"nil slice", f.u1.ID, nil},
		{"empty slice", f.u1.ID, []string{}},
		{"unknown post ids", f.u1.ID, []string{"no-such-post"}},
		{"unknown user", "no-such-user", []string{f.postA}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.repo.BatchGetUserReactions(context.Background(), tt.userID, tt.postIDs)
			if err != nil {
				t.Fatalf("BatchGetUserReactions: %v", err)
			}
			if len(got) != 0 {
				t.Errorf("got %v, want an empty result", got)
			}
		})
	}
}

func sortedTypes(types []domain.ReactionType) []string {
	out := make([]string, len(types))
	for i, rt := range types {
		out[i] = string(rt)
	}
	sort.Strings(out)
	return out
}

// --- single-post reads ---

func TestReactionRepo_CountByPost(t *testing.T) {
	f := newReactionFixture(t)
	ctx := context.Background()

	counts, err := f.repo.CountByPost(ctx, f.postA)
	if err != nil {
		t.Fatalf("CountByPost: %v", err)
	}
	if counts[domain.ReactionBell] != 3 || counts[domain.ReactionHeart] != 1 {
		t.Errorf("postA counts = %v, want bell:3 heart:1", counts)
	}
	if len(counts) != 2 {
		t.Errorf("postA has %d reaction types, want 2", len(counts))
	}

	// The single-post and batch paths are separate SQL; they must agree.
	batch, err := f.repo.BatchCountByPosts(ctx, []string{f.postA})
	if err != nil {
		t.Fatalf("BatchCountByPosts: %v", err)
	}
	for rt, n := range counts {
		if batch[f.postA][rt] != n {
			t.Errorf("%s: CountByPost says %d, BatchCountByPosts says %d", rt, n, batch[f.postA][rt])
		}
	}
}

func TestReactionRepo_CountByPost_NoReactions(t *testing.T) {
	f := newReactionFixture(t)

	counts, err := f.repo.CountByPost(context.Background(), f.postC)
	if err != nil {
		t.Fatalf("CountByPost: %v", err)
	}
	if len(counts) != 0 {
		t.Errorf("counts = %v, want empty", counts)
	}
}

func TestReactionRepo_ListByPost(t *testing.T) {
	f := newReactionFixture(t)
	ctx := context.Background()

	reactions, err := f.repo.ListByPost(ctx, f.postA)
	if err != nil {
		t.Fatalf("ListByPost: %v", err)
	}
	if len(reactions) != 4 {
		t.Fatalf("got %d reactions for postA, want 4", len(reactions))
	}
	for _, r := range reactions {
		if r.PostID != f.postA {
			t.Errorf("reaction %s belongs to post %s, want %s", r.ID, r.PostID, f.postA)
		}
		if r.ID == "" || r.UserID == "" || r.Type == "" {
			t.Errorf("reaction came back incompletely populated: %+v", r)
		}
		if r.CreatedAt.IsZero() {
			t.Errorf("reaction %s has a zero CreatedAt", r.ID)
		}
	}

	empty, err := f.repo.ListByPost(ctx, f.postC)
	if err != nil {
		t.Fatalf("ListByPost: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("postC has %d reactions, want none", len(empty))
	}
}

func TestReactionRepo_GetUserReaction(t *testing.T) {
	f := newReactionFixture(t)
	ctx := context.Background()

	got, err := f.repo.GetUserReaction(ctx, f.u1.ID, f.postA, domain.ReactionBell)
	if err != nil {
		t.Fatalf("GetUserReaction: %v", err)
	}
	if got == nil {
		t.Fatal("got nil, want u1's bell on postA")
	}
	if got.UserID != f.u1.ID || got.PostID != f.postA || got.Type != domain.ReactionBell {
		t.Errorf("got %+v, want u1/postA/bell", got)
	}
}

// A reaction that was never made is reported as (nil, nil) rather than an
// error — "has this user reacted?" is a question, not a failure.
func TestReactionRepo_GetUserReaction_Missing(t *testing.T) {
	f := newReactionFixture(t)
	ctx := context.Background()

	tests := []struct {
		name           string
		userID, postID string
		rt             domain.ReactionType
	}{
		{"right user, wrong type", f.u1.ID, f.postA, domain.ReactionCelebrate},
		{"right user, post they skipped", f.u1.ID, f.postB, domain.ReactionBell},
		{"a user who reacted to nothing", f.author.ID, f.postA, domain.ReactionBell},
		{"unknown post", f.u1.ID, "no-such-post", domain.ReactionBell},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.repo.GetUserReaction(ctx, tt.userID, tt.postID, tt.rt)
			if err != nil {
				t.Fatalf("GetUserReaction: %v", err)
			}
			if got != nil {
				t.Errorf("got %+v, want nil", got)
			}
		})
	}
}

// --- writes ---

func TestReactionRepo_RemoveReaction(t *testing.T) {
	f := newReactionFixture(t)
	ctx := context.Background()

	if err := f.repo.RemoveReaction(ctx, f.u1.ID, f.postA, domain.ReactionBell); err != nil {
		t.Fatalf("RemoveReaction: %v", err)
	}

	gone, err := f.repo.GetUserReaction(ctx, f.u1.ID, f.postA, domain.ReactionBell)
	if err != nil {
		t.Fatalf("GetUserReaction: %v", err)
	}
	if gone != nil {
		t.Error("reaction survived RemoveReaction")
	}

	// Only that one reaction went: u1's heart and the other users' bells stay.
	counts, err := f.repo.CountByPost(ctx, f.postA)
	if err != nil {
		t.Fatalf("CountByPost: %v", err)
	}
	if counts[domain.ReactionBell] != 2 || counts[domain.ReactionHeart] != 1 {
		t.Errorf("postA counts = %v, want bell:2 heart:1", counts)
	}
}

// Removing something that was never there is not an error: the endpoint is
// idempotent, so a double-tap or a retry must not 500.
func TestReactionRepo_RemoveReaction_NotPresentIsNotAnError(t *testing.T) {
	f := newReactionFixture(t)
	ctx := context.Background()

	if err := f.repo.RemoveReaction(ctx, f.u1.ID, f.postC, domain.ReactionBell); err != nil {
		t.Errorf("removing a reaction that does not exist: %v", err)
	}
	if err := f.repo.RemoveReaction(ctx, "no-such-user", f.postA, domain.ReactionBell); err != nil {
		t.Errorf("removing for an unknown user: %v", err)
	}
}

func TestReactionRepo_AddReaction_RoundTrips(t *testing.T) {
	f := newReactionFixture(t)
	ctx := context.Background()

	added := newReaction(t, f.pool, f.u3.ID, f.postC, domain.ReactionCelebrate)

	got, err := f.repo.GetUserReaction(ctx, f.u3.ID, f.postC, domain.ReactionCelebrate)
	if err != nil {
		t.Fatalf("GetUserReaction: %v", err)
	}
	if got == nil {
		t.Fatal("reaction not found after AddReaction")
	}
	if got.ID != added.ID {
		t.Errorf("id = %q, want %q", got.ID, added.ID)
	}
	if !got.CreatedAt.Equal(added.CreatedAt.UTC().Truncate(time.Microsecond)) &&
		got.CreatedAt.Sub(added.CreatedAt).Abs() > time.Millisecond {
		t.Errorf("created_at = %v, want ~%v", got.CreatedAt, added.CreatedAt)
	}
}

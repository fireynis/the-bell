//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/fireynis/the-bell/internal/testsupport"
	"github.com/jackc/pgx/v5"
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

	// Totals as well as the per-type breakdown: a GROUP BY that split or merged
	// rows wrongly could still land the individual numbers while changing how
	// many types a post reports.
	wantTypes := map[string]int{f.postA: 2, f.postB: 1}
	for postID, n := range wantTypes {
		if len(got[postID]) != n {
			t.Errorf("post %s reports %d reaction types, want %d", postID, len(got[postID]), n)
		}
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

// --- writes ---

// storedReaction reads a single reaction straight out of the table.
//
// The repository has no single-post read — the feed only ever loads reactions
// in batches, so one was never wired — and the batch queries return neither the
// id nor created_at. A write test still has to see what actually landed, so it
// asks the database directly rather than growing a production method that only
// tests would call.
func storedReaction(t *testing.T, pool *pgxpool.Pool, userID, postID string, rt domain.ReactionType) (id string, createdAt time.Time, found bool) {
	t.Helper()

	err := pool.QueryRow(context.Background(),
		`SELECT id, created_at FROM reactions WHERE user_id = $1 AND post_id = $2 AND reaction_type = $3`,
		userID, postID, string(rt),
	).Scan(&id, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, false
	}
	if err != nil {
		t.Fatalf("reading reaction %s/%s/%s: %v", userID, postID, rt, err)
	}
	return id, createdAt, true
}

func TestReactionRepo_RemoveReaction(t *testing.T) {
	f := newReactionFixture(t)
	ctx := context.Background()

	if err := f.repo.RemoveReaction(ctx, f.u1.ID, f.postA, domain.ReactionBell); err != nil {
		t.Fatalf("RemoveReaction: %v", err)
	}

	if _, _, found := storedReaction(t, f.pool, f.u1.ID, f.postA, domain.ReactionBell); found {
		t.Error("reaction survived RemoveReaction")
	}

	// Only that one reaction went: u1's heart and the other users' bells stay.
	counts, err := f.repo.BatchCountByPosts(ctx, []string{f.postA})
	if err != nil {
		t.Fatalf("BatchCountByPosts: %v", err)
	}
	if counts[f.postA][domain.ReactionBell] != 2 || counts[f.postA][domain.ReactionHeart] != 1 {
		t.Errorf("postA counts = %v, want bell:2 heart:1", counts[f.postA])
	}

	// And u1 keeps the heart they did not remove.
	mine, err := f.repo.BatchGetUserReactions(ctx, f.u1.ID, []string{f.postA})
	if err != nil {
		t.Fatalf("BatchGetUserReactions: %v", err)
	}
	if types := sortedTypes(mine[f.postA]); len(types) != 1 || types[0] != string(domain.ReactionHeart) {
		t.Errorf("u1 postA = %v, want [heart]", types)
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

// Reacting to a post that does not exist trips the reactions_post_id_fkey
// foreign key. The ON CONFLICT clause on AddReaction resolves an index
// conflict, so it swallows a repeat reaction — but a foreign key is a
// referential trigger it cannot reach, and the raw pgx error surfaced as a 500.
// The caller sent a well-formed request naming a post that is not there, which
// is a 404, so the adapter maps it to service.ErrNotFound.
func TestReactionRepo_AddReaction_MissingPostIsNotFound(t *testing.T) {
	f := newReactionFixture(t)

	err := f.repo.AddReaction(context.Background(), &domain.Reaction{
		ID:        "reaction-missing-post",
		UserID:    f.u1.ID,
		PostID:    "no-such-post",
		Type:      domain.ReactionBell,
		CreatedAt: time.Now(),
	})
	if !errors.Is(err, service.ErrNotFound) {
		t.Errorf("err = %v, want service.ErrNotFound", err)
	}
}

func TestReactionRepo_AddReaction_RoundTrips(t *testing.T) {
	f := newReactionFixture(t)
	ctx := context.Background()

	added := newReaction(t, f.pool, f.u3.ID, f.postC, domain.ReactionCelebrate)

	gotID, gotCreatedAt, found := storedReaction(t, f.pool, f.u3.ID, f.postC, domain.ReactionCelebrate)
	if !found {
		t.Fatal("reaction not found after AddReaction")
	}
	if gotID != added.ID {
		t.Errorf("id = %q, want %q", gotID, added.ID)
	}
	if !gotCreatedAt.Equal(added.CreatedAt.UTC().Truncate(time.Microsecond)) &&
		gotCreatedAt.Sub(added.CreatedAt).Abs() > time.Millisecond {
		t.Errorf("created_at = %v, want ~%v", gotCreatedAt, added.CreatedAt)
	}

	// It also reaches the batch reads the feed actually uses.
	mine, err := f.repo.BatchGetUserReactions(ctx, f.u3.ID, []string{f.postC})
	if err != nil {
		t.Fatalf("BatchGetUserReactions: %v", err)
	}
	if len(mine[f.postC]) != 1 || mine[f.postC][0] != domain.ReactionCelebrate {
		t.Errorf("u3 postC = %v, want [celebrate]", mine[f.postC])
	}
}

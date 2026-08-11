//go:build integration

// These tests live in package postgres_test rather than package postgres:
// testsupport imports this package for its fixtures, so an internal test file
// importing testsupport would close an import cycle.
package postgres_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/fireynis/the-bell/internal/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMain(m *testing.M) { os.Exit(testsupport.RunMain(m)) }

// The three adapters under test are the read side of the composite trust
// calculation, so each one is checked against a real database rather than a
// fake: the window boundary and the 'visible' filter live in the SQL, not in
// the Go code, and a fake would assert only that the fake works.

func newPost(t *testing.T, pool *pgxpool.Pool, id, authorID string, status domain.PostStatus, createdAt time.Time) *domain.Post {
	t.Helper()

	post := &domain.Post{
		ID:        id,
		AuthorID:  authorID,
		Body:      "body of " + id,
		Status:    status,
		CreatedAt: createdAt,
	}
	if err := postgres.NewPostRepo(postgres.New(pool)).CreatePost(context.Background(), post); err != nil {
		t.Fatalf("creating post %s: %v", id, err)
	}
	return post
}

func TestPostRepo_CountPostsByAuthorSince(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewPostRepo(postgres.New(pool))

	now := time.Now()
	author := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("author"), domain.RoleMember, 50)
	other := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("other"), domain.RoleMember, 50)

	newPost(t, pool, "p-recent-1", author.ID, domain.PostVisible, now.Add(-1*time.Hour))
	newPost(t, pool, "p-recent-2", author.ID, domain.PostVisible, now.Add(-2*time.Hour))
	// Outside the window, so excluded.
	newPost(t, pool, "p-old", author.ID, domain.PostVisible, now.Add(-48*time.Hour))
	// Removed posts do not count toward the author's activity.
	newPost(t, pool, "p-removed", author.ID, domain.PostRemovedByMod, now.Add(-1*time.Hour))
	// A different author's post must not leak in.
	newPost(t, pool, "p-other", other.ID, domain.PostVisible, now.Add(-1*time.Hour))

	got, err := repo.CountPostsByAuthorSince(ctx, author.ID, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("CountPostsByAuthorSince: %v", err)
	}
	if got != 2 {
		t.Errorf("CountPostsByAuthorSince = %d, want 2", got)
	}

	got, err = repo.CountPostsByAuthorSince(ctx, other.ID, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("CountPostsByAuthorSince: %v", err)
	}
	if got != 1 {
		t.Errorf("CountPostsByAuthorSince for the other author = %d, want 1", got)
	}
}

func TestPostRepo_CountPostsByAuthorSince_NoPosts(t *testing.T) {
	pool := testsupport.TestDB(t)
	repo := postgres.NewPostRepo(postgres.New(pool))

	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("quiet"), domain.RoleMember, 50)

	got, err := repo.CountPostsByAuthorSince(context.Background(), user.ID, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("CountPostsByAuthorSince: %v", err)
	}
	if got != 0 {
		t.Errorf("CountPostsByAuthorSince = %d, want 0", got)
	}
}

func TestReactionRepo_CountReactionsReceivedByAuthorSince(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	q := postgres.New(pool)
	reactions := postgres.NewReactionRepo(q)

	now := time.Now()
	author := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("author"), domain.RoleMember, 50)
	other := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("other"), domain.RoleMember, 50)
	reactor := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("reactor"), domain.RoleMember, 50)

	newPost(t, pool, "p-author", author.ID, domain.PostVisible, now.Add(-3*time.Hour))
	newPost(t, pool, "p-other", other.ID, domain.PostVisible, now.Add(-3*time.Hour))

	// Counted: two reactions on the author's post inside the window. They must
	// differ in type, since one user may react to a post only once per type.
	addReaction(t, reactions, "r-1", reactor.ID, "p-author", domain.ReactionBell, now.Add(-1*time.Hour))
	addReaction(t, reactions, "r-2", reactor.ID, "p-author", domain.ReactionHeart, now.Add(-2*time.Hour))
	// Outside the window.
	addReaction(t, reactions, "r-old", reactor.ID, "p-author", domain.ReactionCelebrate, now.Add(-48*time.Hour))
	// On somebody else's post.
	addReaction(t, reactions, "r-other", reactor.ID, "p-other", domain.ReactionBell, now.Add(-1*time.Hour))

	got, err := reactions.CountReactionsReceivedByAuthorSince(ctx, author.ID, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("CountReactionsReceivedByAuthorSince: %v", err)
	}
	if got != 2 {
		t.Errorf("CountReactionsReceivedByAuthorSince = %d, want 2", got)
	}
}

func addReaction(t *testing.T, repo *postgres.ReactionRepo, id, userID, postID string, reactionType domain.ReactionType, createdAt time.Time) {
	t.Helper()

	err := repo.AddReaction(context.Background(), &domain.Reaction{
		ID:        id,
		UserID:    userID,
		PostID:    postID,
		Type:      reactionType,
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("adding reaction %s: %v", id, err)
	}
}

func TestVouchRepo_CountActiveVouchesWithAvgTrust(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewVouchRepo(postgres.New(pool))

	now := time.Now()
	vouchee := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("vouchee"), domain.RoleMember, 50)
	high := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("high"), domain.RoleMember, 80)
	low := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("low"), domain.RoleMember, 40)
	revoker := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("revoker"), domain.RoleMember, 100)

	addVouch(t, repo, "v-high", high.ID, vouchee.ID, domain.VouchActive, now)
	addVouch(t, repo, "v-low", low.ID, vouchee.ID, domain.VouchActive, now)
	// A revoked vouch must contribute to neither the count nor the average —
	// the revoker's trust of 100 would visibly skew the mean if it leaked in.
	addVouch(t, repo, "v-revoked", revoker.ID, vouchee.ID, domain.VouchRevoked, now)

	count, avgTrust, err := repo.CountActiveVouchesWithAvgTrust(ctx, vouchee.ID)
	if err != nil {
		t.Fatalf("CountActiveVouchesWithAvgTrust: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if avgTrust < 59.999 || avgTrust > 60.001 {
		t.Errorf("avgTrust = %f, want ~60 (mean of 80 and 40)", avgTrust)
	}
}

// With no vouchers there is no mean to take, so the query coalesces the average
// to 0. Callers rely on that rather than having to handle a NULL.
func TestVouchRepo_CountActiveVouchesWithAvgTrust_NoVouches(t *testing.T) {
	pool := testsupport.TestDB(t)
	repo := postgres.NewVouchRepo(postgres.New(pool))

	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("unvouched"), domain.RoleMember, 50)

	count, avgTrust, err := repo.CountActiveVouchesWithAvgTrust(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("CountActiveVouchesWithAvgTrust: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
	if avgTrust != 0 {
		t.Errorf("avgTrust = %f, want 0", avgTrust)
	}
}

// CreateVouch maps the uq_voucher_vouchee violation to service.ErrValidation so
// a duplicate vouch surfaces as a user error rather than a 500.
func TestVouchRepo_CreateVouch_DuplicateIsValidationError(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewVouchRepo(postgres.New(pool))

	now := time.Now()
	voucher := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("voucher"), domain.RoleMember, 50)
	vouchee := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("vouchee"), domain.RoleMember, 50)

	addVouch(t, repo, "v-first", voucher.ID, vouchee.ID, domain.VouchActive, now)

	err := repo.CreateVouch(ctx, &domain.Vouch{
		ID:        "v-duplicate",
		VoucherID: voucher.ID,
		VoucheeID: vouchee.ID,
		Status:    domain.VouchActive,
		CreatedAt: now,
	})
	if !errors.Is(err, service.ErrValidation) {
		t.Fatalf("CreateVouch on a duplicate = %v, want service.ErrValidation", err)
	}
}

// AddReaction needs no such mapping: its query is an upsert
// (ON CONFLICT (user_id, post_id, reaction_type) DO UPDATE), so reacting twice
// cannot raise 23505 at all. It is silently idempotent, leaving one row with
// the original timestamp. This test pins that down, because the alternative
// reading — that a double-tap is an error the caller must handle — would put
// dead error-mapping code in the adapter.
//
// The upsert covers only that unique index, though. AddReaction does map the
// post_id foreign key (23503), because ON CONFLICT resolves an index conflict
// while a foreign key is a referential trigger that fires regardless — see
// TestReactionRepo_AddReaction_MissingPostIsNotFound. The two codes look alike
// and behave completely differently here, so neither guard should be inferred
// from the other.
func TestReactionRepo_AddReaction_DuplicateIsIdempotent(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewReactionRepo(postgres.New(pool))

	now := time.Now().Truncate(time.Millisecond)
	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("reactor"), domain.RoleMember, 50)
	newPost(t, pool, "p-1", user.ID, domain.PostVisible, now)

	addReaction(t, repo, "r-first", user.ID, "p-1", domain.ReactionBell, now)

	err := repo.AddReaction(ctx, &domain.Reaction{
		ID:        "r-duplicate",
		UserID:    user.ID,
		PostID:    "p-1",
		Type:      domain.ReactionBell,
		CreatedAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("AddReaction on a duplicate = %v, want no error", err)
	}

	counts, err := repo.BatchCountByPosts(ctx, []string{"p-1"})
	if err != nil {
		t.Fatalf("BatchCountByPosts: %v", err)
	}
	if counts["p-1"][domain.ReactionBell] != 1 {
		t.Errorf("bell reactions = %d, want 1", counts["p-1"][domain.ReactionBell])
	}

	// DO UPDATE SET created_at = reactions.created_at keeps the original row,
	// so the second call must not move the timestamp forward — and the row that
	// survived is the first one, under its original id.
	storedID, storedCreatedAt, found := storedReaction(t, pool, user.ID, "p-1", domain.ReactionBell)
	if !found {
		t.Fatal("no reaction row survived the upsert")
	}
	if storedID != "r-first" {
		t.Errorf("stored id = %q, want the original %q", storedID, "r-first")
	}
	if !storedCreatedAt.Equal(now) {
		t.Errorf("stored CreatedAt = %v, want the original %v", storedCreatedAt, now)
	}
}

func addVouch(t *testing.T, repo *postgres.VouchRepo, id, voucherID, voucheeID string, status domain.VouchStatus, createdAt time.Time) {
	t.Helper()

	err := repo.CreateVouch(context.Background(), &domain.Vouch{
		ID:        id,
		VoucherID: voucherID,
		VoucheeID: voucheeID,
		Status:    status,
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("creating vouch %s: %v", id, err)
	}
}

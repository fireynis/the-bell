//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/testsupport"
	"github.com/google/uuid"
)

// moderation_reliefs is the durable record of a moderator lifting a restriction
// rather than imposing one. It is read by the released member's own profile, so
// a field lost in the round trip is a member being told the wrong thing about
// their own moderation record.

func TestModerationReliefRepo_RoundTrip(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewModerationReliefRepo(postgres.New(pool))

	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("relief-target"), domain.RoleMember, 50)
	mod := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("relief-mod"), domain.RoleModerator, 90)

	// Truncated because Postgres timestamptz keeps microseconds, not
	// nanoseconds: an untruncated value would fail on the round trip for a
	// reason that has nothing to do with the code under test.
	previous := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Microsecond)
	createdAt := time.Now().UTC().Truncate(time.Microsecond)

	want := &domain.ModerationRelief{
		ID:                uuid.NewString(),
		TargetUserID:      target.ID,
		ModeratorID:       mod.ID,
		Type:              domain.ReliefMuteLift,
		PreviousExpiresAt: &previous,
		WasInForce:        true,
		CreatedAt:         createdAt,
	}
	if err := repo.CreateModerationRelief(ctx, want); err != nil {
		t.Fatalf("CreateModerationRelief: %v", err)
	}

	got, err := repo.ListMuteLiftsInForce(ctx, target.ID, 10)
	if err != nil {
		t.Fatalf("ListMuteLiftsInForce: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("%d reliefs, want 1", len(got))
	}

	r := got[0]
	if r.ID != want.ID {
		t.Errorf("id = %q, want %q", r.ID, want.ID)
	}
	if r.TargetUserID != target.ID {
		t.Errorf("target = %q, want %q", r.TargetUserID, target.ID)
	}
	if r.ModeratorID != mod.ID {
		t.Errorf("moderator = %q, want %q", r.ModeratorID, mod.ID)
	}
	if r.Type != domain.ReliefMuteLift {
		t.Errorf("type = %q, want %q", r.Type, domain.ReliefMuteLift)
	}
	if !r.WasInForce {
		t.Error("was_in_force came back false")
	}
	if r.PreviousExpiresAt == nil {
		t.Fatal("previous_expires_at came back nil")
	}
	if !r.PreviousExpiresAt.Equal(previous) {
		t.Errorf("previous_expires_at = %v, want %v", *r.PreviousExpiresAt, previous)
	}
	if !r.CreatedAt.Equal(createdAt) {
		t.Errorf("created_at = %v, want %v", r.CreatedAt, createdAt)
	}
}

// A lift against somebody who was not muted has no previous expiry. It must
// read back as nil rather than as the zero time, which would claim they were
// muted until year 1 — the same failure muted_until's own default guards
// against.
func TestModerationReliefRepo_NullPreviousExpiryIsNotZeroTime(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewModerationReliefRepo(postgres.New(pool))

	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("relief-null"), domain.RoleMember, 50)
	mod := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("relief-null-mod"), domain.RoleModerator, 90)

	err := repo.CreateModerationRelief(ctx, &domain.ModerationRelief{
		ID:           uuid.NewString(),
		TargetUserID: target.ID,
		ModeratorID:  mod.ID,
		Type:         domain.ReliefMuteLift,
		WasInForce:   false,
		CreatedAt:    time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("CreateModerationRelief: %v", err)
	}

	// Read straight from the table: ListMuteLiftsInForce deliberately excludes
	// this row, and what is under test is what was persisted.
	var (
		previous   *time.Time
		wasInForce bool
	)
	err = pool.QueryRow(ctx,
		`SELECT previous_expires_at, was_in_force FROM moderation_reliefs WHERE target_user_id = $1`,
		target.ID,
	).Scan(&previous, &wasInForce)
	if err != nil {
		t.Fatalf("reading the relief row: %v", err)
	}
	if previous != nil {
		t.Errorf("previous_expires_at = %v, want NULL", *previous)
	}
	if wasInForce {
		t.Error("was_in_force is true for a lift that freed nobody")
	}

	// And it is excluded from the member's view: they were never muted, so they
	// must not be told they were released.
	got, err := repo.ListMuteLiftsInForce(ctx, target.ID, 10)
	if err != nil {
		t.Fatalf("ListMuteLiftsInForce: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("%d lifts in the member's view, want none", len(got))
	}
}

// The was_in_force filter has to run in the query, before LIMIT. Applied to an
// already-limited result set instead, a run of idempotent lifts — which a
// moderator can issue against anybody, and which the endpoint accepts — would
// push the release that actually freed somebody out of the window, and the
// member would be told they had never been released.
func TestModerationReliefRepo_NoOpLiftsCannotHideARealOne(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewModerationReliefRepo(postgres.New(pool))

	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("relief-crowd"), domain.RoleMember, 50)
	mod := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("relief-crowd-mod"), domain.RoleModerator, 90)

	base := time.Now().UTC().Truncate(time.Microsecond)
	previous := base.Add(time.Hour)

	// The real release is the oldest row.
	err := repo.CreateModerationRelief(ctx, &domain.ModerationRelief{
		ID: uuid.NewString(), TargetUserID: target.ID, ModeratorID: mod.ID,
		Type: domain.ReliefMuteLift, PreviousExpiresAt: &previous,
		WasInForce: true, CreatedAt: base,
	})
	if err != nil {
		t.Fatalf("CreateModerationRelief: %v", err)
	}

	// Then five lifts against somebody who is no longer muted, all newer.
	for i := 1; i <= 5; i++ {
		err := repo.CreateModerationRelief(ctx, &domain.ModerationRelief{
			ID: uuid.NewString(), TargetUserID: target.ID, ModeratorID: mod.ID,
			Type: domain.ReliefMuteLift, WasInForce: false,
			CreatedAt: base.Add(time.Duration(i) * time.Hour),
		})
		if err != nil {
			t.Fatalf("CreateModerationRelief %d: %v", i, err)
		}
	}

	got, err := repo.ListMuteLiftsInForce(ctx, target.ID, 3)
	if err != nil {
		t.Fatalf("ListMuteLiftsInForce: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("%d lifts, want the 1 that actually released them", len(got))
	}
	if !got[0].CreatedAt.Equal(base) {
		t.Errorf("lift created_at = %v, want the real release at %v", got[0].CreatedAt, base)
	}
}

// Newest first, and bounded: the member's profile asks for a handful, and the
// handful it gets must be the most recent ones.
func TestModerationReliefRepo_ListsNewestFirstWithinTheLimit(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewModerationReliefRepo(postgres.New(pool))

	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("relief-order"), domain.RoleMember, 50)
	mod := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("relief-order-mod"), domain.RoleModerator, 90)

	base := time.Now().UTC().Truncate(time.Microsecond)
	// Written oldest-first so a repository that simply returned insertion order
	// would fail rather than pass by luck.
	for i := range 3 {
		err := repo.CreateModerationRelief(ctx, &domain.ModerationRelief{
			ID:           uuid.NewString(),
			TargetUserID: target.ID,
			ModeratorID:  mod.ID,
			Type:         domain.ReliefMuteLift,
			WasInForce:   true,
			CreatedAt:    base.Add(time.Duration(i) * time.Hour),
		})
		if err != nil {
			t.Fatalf("CreateModerationRelief %d: %v", i, err)
		}
	}

	got, err := repo.ListMuteLiftsInForce(ctx, target.ID, 2)
	if err != nil {
		t.Fatalf("ListMuteLiftsInForce: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("%d reliefs, want the limit of 2", len(got))
	}
	if !got[0].CreatedAt.Equal(base.Add(2 * time.Hour)) {
		t.Errorf("first relief created_at = %v, want the newest %v", got[0].CreatedAt, base.Add(2*time.Hour))
	}
	if !got[1].CreatedAt.Equal(base.Add(time.Hour)) {
		t.Errorf("second relief created_at = %v, want %v", got[1].CreatedAt, base.Add(time.Hour))
	}
}

// One member's reliefs must never appear on another's profile.
func TestModerationReliefRepo_ScopedToTheTarget(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewModerationReliefRepo(postgres.New(pool))

	mine := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("relief-mine"), domain.RoleMember, 50)
	theirs := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("relief-theirs"), domain.RoleMember, 50)
	mod := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("relief-scope-mod"), domain.RoleModerator, 90)

	err := repo.CreateModerationRelief(ctx, &domain.ModerationRelief{
		ID: uuid.NewString(), TargetUserID: theirs.ID, ModeratorID: mod.ID,
		Type: domain.ReliefMuteLift, WasInForce: true, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("CreateModerationRelief: %v", err)
	}

	got, err := repo.ListMuteLiftsInForce(ctx, mine.ID, 10)
	if err != nil {
		t.Fatalf("ListMuteLiftsInForce: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("%d reliefs on a member who was never released", len(got))
	}
}

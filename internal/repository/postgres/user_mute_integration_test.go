//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/testsupport"
)

// Migrations 00001 and 00007 switch search_path to ag_catalog for their AGE
// work and switch it back at the end. If a future AGE migration forgets the
// reset, every table and column created after it lands in ag_catalog instead of
// public — the application then fails at runtime against a schema that migrated
// without error. muted_until is the first column added after that hazard, so it
// is the one that checks it, and this asserts against a real migrated database
// rather than by reading the SQL.
func TestMigrations_ColumnsLandInPublicSchema(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()

	// One column from before the AGE migrations and one from after, so a broken
	// reset shows up as a difference between them rather than as a uniform
	// failure that could equally mean the query is wrong.
	tests := []struct {
		table  string
		column string
	}{
		{"users", "id"},
		{"users", "muted_until"},
		{"users", "trust_below_since"},
		{"posts", "status"},
		{"role_history", "new_role"},
	}

	for _, tt := range tests {
		t.Run(tt.table+"."+tt.column, func(t *testing.T) {
			var schema string
			err := pool.QueryRow(ctx,
				`SELECT table_schema FROM information_schema.columns
				 WHERE table_name = $1 AND column_name = $2`,
				tt.table, tt.column,
			).Scan(&schema)
			if err != nil {
				t.Fatalf("looking up %s.%s: %v", tt.table, tt.column, err)
			}
			if schema != "public" {
				t.Errorf("%s.%s is in schema %q, want public — an AGE migration left search_path pointing at %s",
					tt.table, tt.column, schema, schema)
			}
		})
	}
}

// muted_until is nullable and defaults to NULL: every user who existed before
// the migration must read back as not muted rather than as muted since the zero
// time.
func TestUserRepo_MutedUntil_DefaultsToNotMuted(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewUserRepo(postgres.New(pool))

	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("mute-default"), domain.RoleMember, 50)

	got, err := repo.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if got.MutedUntil != nil {
		t.Errorf("muted_until = %v on a fresh user, want nil", *got.MutedUntil)
	}
	if !got.CanPost(time.Now()) {
		t.Error("a fresh member cannot post")
	}
}

func TestUserRepo_SetUserMutedUntil_RoundTrips(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewUserRepo(postgres.New(pool))

	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("mute-set"), domain.RoleMember, 50)
	until := time.Now().Add(time.Hour).Truncate(time.Microsecond)

	if err := repo.SetUserMutedUntil(ctx, user.ID, &until); err != nil {
		t.Fatalf("SetUserMutedUntil: %v", err)
	}

	got, err := repo.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if got.MutedUntil == nil {
		t.Fatal("muted_until came back nil after being set")
	}
	// Postgres stores timestamptz at microsecond resolution.
	if diff := got.MutedUntil.Sub(until).Abs(); diff > time.Microsecond {
		t.Errorf("muted_until = %v, want %v (off by %v)", got.MutedUntil, until, diff)
	}

	// The mute is what stops the post, not the score: trust is untouched.
	if got.TrustScore != 50 {
		t.Errorf("trust score = %v, want it untouched at 50", got.TrustScore)
	}
	if got.CanPost(until.Add(-time.Minute)) {
		t.Error("CanPost() = true during an active mute")
	}
	if !got.CanPost(until.Add(time.Minute)) {
		t.Error("CanPost() = false after the mute expired")
	}
}

// Setting NULL lifts the mute, which is how a moderator releases someone early.
func TestUserRepo_SetUserMutedUntil_NilLiftsTheMute(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewUserRepo(postgres.New(pool))

	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("mute-lift"), domain.RoleMember, 50)
	until := time.Now().Add(time.Hour)

	if err := repo.SetUserMutedUntil(ctx, user.ID, &until); err != nil {
		t.Fatalf("SetUserMutedUntil: %v", err)
	}
	if err := repo.SetUserMutedUntil(ctx, user.ID, nil); err != nil {
		t.Fatalf("SetUserMutedUntil(nil): %v", err)
	}

	got, err := repo.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if got.MutedUntil != nil {
		t.Errorf("muted_until = %v after being cleared, want nil", *got.MutedUntil)
	}
	if !got.CanPost(time.Now()) {
		t.Error("a released user still cannot post")
	}
}

// A second mute overwrites rather than extends, so a moderator issuing a
// shorter mute over a longer one gets the length they chose.
func TestUserRepo_SetUserMutedUntil_Overwrites(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewUserRepo(postgres.New(pool))

	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("mute-overwrite"), domain.RoleMember, 50)
	long := time.Now().Add(30 * 24 * time.Hour).Truncate(time.Microsecond)
	short := time.Now().Add(time.Hour).Truncate(time.Microsecond)

	if err := repo.SetUserMutedUntil(ctx, user.ID, &long); err != nil {
		t.Fatalf("SetUserMutedUntil: %v", err)
	}
	if err := repo.SetUserMutedUntil(ctx, user.ID, &short); err != nil {
		t.Fatalf("SetUserMutedUntil: %v", err)
	}

	got, err := repo.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if got.MutedUntil == nil {
		t.Fatal("muted_until came back nil")
	}
	if diff := got.MutedUntil.Sub(short).Abs(); diff > time.Microsecond {
		t.Errorf("muted_until = %v, want the shorter %v", got.MutedUntil, short)
	}
}

// The mute must survive the reads the rest of the application uses, not just
// GetUserByID — the auth middleware looks users up by Kratos id on every
// request, and that is the copy CanPost is called on.
func TestUserRepo_MutedUntil_ReachesEveryUserRead(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewUserRepo(postgres.New(pool))

	kratosID := testsupport.UniqueKratosID("mute-reads")
	user := testsupport.TestUser(t, pool, kratosID, domain.RoleMember, 50)
	until := time.Now().Add(time.Hour)

	if err := repo.SetUserMutedUntil(ctx, user.ID, &until); err != nil {
		t.Fatalf("SetUserMutedUntil: %v", err)
	}

	byID, err := repo.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	byKratos, err := repo.GetUserByKratosID(ctx, kratosID)
	if err != nil {
		t.Fatalf("GetUserByKratosID: %v", err)
	}

	for name, got := range map[string]*domain.User{"GetUserByID": byID, "GetUserByKratosID": byKratos} {
		if got.MutedUntil == nil {
			t.Errorf("%s: muted_until is nil, so the mute would not be enforced on this path", name)
			continue
		}
		if got.CanPost(time.Now()) {
			t.Errorf("%s: CanPost() = true during an active mute", name)
		}
	}
}

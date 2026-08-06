//go:build integration

package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/testsupport"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
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
		// Altered by 00015, which runs after the AGE migrations: if search_path
		// were left pointing at ag_catalog the ALTER would miss this table.
		{"trust_penalties", "moderation_action_id"},
		// Added by 00017, the newest column after the AGE hazard.
		{"posts", "removed_by"},
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

// Migration 00015 dropped NOT NULL on trust_penalties.moderation_action_id so
// that a penalty which no moderator caused — revoking a vouch costs the voucher
// -3 for 30 days — can be written at all. Before it, both an empty string and a
// synthetic id failed the foreign key, so the design doc's revocation penalty
// was unimplementable.
func TestMigrations_PenaltyMayHaveNoModerationAction(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()

	var nullable string
	err := pool.QueryRow(ctx,
		`SELECT is_nullable FROM information_schema.columns
		 WHERE table_schema = 'public' AND table_name = 'trust_penalties'
		   AND column_name = 'moderation_action_id'`,
	).Scan(&nullable)
	if err != nil {
		t.Fatalf("looking up trust_penalties.moderation_action_id: %v", err)
	}
	if nullable != "YES" {
		t.Errorf("moderation_action_id is_nullable = %q, want YES — a penalty with no moderation action behind it cannot be written", nullable)
	}

	// The foreign key must still hold for every non-NULL value, so moderation
	// penalties stay tied to a real action.
	var constraints int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM information_schema.table_constraints
		 WHERE table_schema = 'public' AND table_name = 'trust_penalties'
		   AND constraint_type = 'FOREIGN KEY'
		   AND constraint_name = 'trust_penalties_moderation_action_id_fkey'`,
	).Scan(&constraints)
	if err != nil {
		t.Fatalf("looking up the foreign key: %v", err)
	}
	if constraints != 1 {
		t.Errorf("found %d moderation_action_id foreign keys, want 1 — dropping NOT NULL must not drop the reference", constraints)
	}
}

// insertPenalty writes a trust_penalties row directly, bypassing the service
// layer, so the constraint itself is what is under test rather than the code
// paths that happen to respect it.
func insertPenalty(ctx context.Context, pool *pgxpool.Pool, userID, actionID string, hopDepth int) error {
	var action any
	if actionID != "" {
		action = actionID
	}
	_, err := pool.Exec(ctx,
		`INSERT INTO trust_penalties
		   (id, user_id, moderation_action_id, penalty_amount, hop_depth, created_at, decays_at)
		 VALUES ($1, $2, $3, 3.0, $4, NOW(), NOW() + INTERVAL '30 days')`,
		uuid.Must(uuid.NewV7()).String(), userID, action, hopDepth,
	)
	return err
}

// seedModerationAction creates a real action row so penalties can reference one.
func seedModerationAction(t *testing.T, pool *pgxpool.Pool, targetID, moderatorID string) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO moderation_actions
		   (id, target_user_id, moderator_id, action_type, severity, reason, created_at)
		 VALUES ($1, $2, $3, 'warn', 2, 'test', NOW())`,
		id, targetID, moderatorID,
	)
	if err != nil {
		t.Fatalf("seeding moderation action: %v", err)
	}
	return id
}

// Migration 00016 restores the half of the traceability guarantee that 00015
// gave up: a propagated penalty must name the action it propagated from.
func TestMigrations_PropagatedPenaltyRequiresAnAction(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()

	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("chk-user"), domain.RoleMember, 50)
	mod := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("chk-mod"), domain.RoleModerator, 90)
	actionID := seedModerationAction(t, pool, user.ID, mod.ID)

	t.Run("constraint exists", func(t *testing.T) {
		var count int
		err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM information_schema.table_constraints
			 WHERE table_schema = 'public' AND table_name = 'trust_penalties'
			   AND constraint_type = 'CHECK'
			   AND constraint_name = 'trust_penalties_propagated_needs_action'`,
		).Scan(&count)
		if err != nil {
			t.Fatalf("looking up the constraint: %v", err)
		}
		if count != 1 {
			t.Errorf("found %d such constraints, want 1", count)
		}
	})

	// The one thing the constraint is for.
	t.Run("propagated penalty with no action is rejected", func(t *testing.T) {
		err := insertPenalty(ctx, pool, user.ID, "", 1)
		if err == nil {
			t.Fatal("a propagated penalty with no moderation action was accepted; it has nothing to trace back to")
		}
		if !strings.Contains(err.Error(), "trust_penalties_propagated_needs_action") {
			t.Errorf("rejected by %v, want the propagated-needs-action check", err)
		}
	})

	// And the three shapes the system legitimately writes must all still pass.
	t.Run("revocation penalty is still accepted", func(t *testing.T) {
		// hop_depth 0, no action: this is the feature 00015 enabled, and the
		// constraint must not have taken it away again.
		if err := insertPenalty(ctx, pool, user.ID, "", 0); err != nil {
			t.Errorf("revocation penalty rejected: %v", err)
		}
	})

	t.Run("direct moderation penalty is still accepted", func(t *testing.T) {
		if err := insertPenalty(ctx, pool, user.ID, actionID, 0); err != nil {
			t.Errorf("direct moderation penalty rejected: %v", err)
		}
	})

	t.Run("propagated moderation penalty is still accepted", func(t *testing.T) {
		if err := insertPenalty(ctx, pool, user.ID, actionID, 2); err != nil {
			t.Errorf("propagated moderation penalty rejected: %v", err)
		}
	})
}

// A CHECK that fails against existing rows is worse than no CHECK: the
// migration would abort on a populated production database while passing on an
// empty test one. This writes every shape the system produces and then re-adds
// the constraint over them, which is what goose does on deploy.
func TestMigrations_PropagatedPenaltyCheckAcceptsExistingRows(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()

	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("live-user"), domain.RoleMember, 50)
	mod := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("live-mod"), domain.RoleModerator, 90)
	actionID := seedModerationAction(t, pool, user.ID, mod.ID)

	// Every shape in the wild: a revocation penalty, a direct moderation
	// penalty, and propagated penalties at both hop depths.
	for _, p := range []struct {
		action   string
		hopDepth int
	}{
		{"", 0},
		{actionID, 0},
		{actionID, 1},
		{actionID, 2},
	} {
		if err := insertPenalty(ctx, pool, user.ID, p.action, p.hopDepth); err != nil {
			t.Fatalf("seeding penalty (action=%q hop=%d): %v", p.action, p.hopDepth, err)
		}
	}

	// Drop and re-add exactly as the migration does. ADD CONSTRAINT validates
	// every existing row, so this fails loudly if the check is too strict.
	if _, err := pool.Exec(ctx,
		`ALTER TABLE trust_penalties DROP CONSTRAINT trust_penalties_propagated_needs_action`,
	); err != nil {
		t.Fatalf("dropping the constraint: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`ALTER TABLE trust_penalties ADD CONSTRAINT trust_penalties_propagated_needs_action
		 CHECK (moderation_action_id IS NOT NULL OR hop_depth = 0)`,
	); err != nil {
		t.Fatalf("re-adding the constraint over existing rows failed, so the migration would abort on a populated database: %v", err)
	}
}

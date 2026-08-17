//go:build integration

// This file is package database_test, not package database, deliberately.
//
// testsupport imports internal/database (it migrates the databases it hands
// out), so an in-package test here could not import testsupport without an
// import cycle. An external test package can, because Go permits foo_test to
// import packages that import foo.
//
// It also means the rollback path used below is built entirely inside the test:
// RunMigrations only ever calls provider.Up, and the brief was explicit that
// verifying Down must not give the bell binary a `migrate down` subcommand. No
// production code was added for any of this.
package database_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/testsupport"
	"github.com/fireynis/the-bell/migrations"
)

// latestVersion is the highest migration version. Kept as a literal so that
// adding a migration without considering its Down block trips this file.
const latestVersion = int64(23)

// testProvider builds a goose provider over the test's own database.
//
// It opens a *sql.DB from the pool because goose speaks database/sql. Every
// migration therefore runs on connections from that DB, which matters for the
// search_path checks below: `SET search_path` is session state, so a migration
// that leaves it pointing somewhere unexpected only affects work that lands on
// the same connection.
func testProvider(t *testing.T, pool *pgxpool.Pool) (*goose.Provider, *sql.DB) {
	t.Helper()

	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })

	// One connection, so a search_path left behind by a Down block is carried
	// into the migrations that follow it exactly as it would be in a real
	// single-connection rollback. A pool that hands out a fresh connection per
	// statement would silently reset search_path and hide the hazard.
	db.SetMaxOpenConns(1)

	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		t.Fatalf("building goose provider: %v", err)
	}
	return provider, db
}

// publicSchemaColumns are the columns TestMigrations_ColumnsLandInPublicSchema
// asserts on. A round trip has to leave every one of them where it found it.
var publicSchemaColumns = []struct{ table, column string }{
	{"users", "id"},
	{"users", "muted_until"},
	{"users", "trust_below_since"},
	{"posts", "status"},
	{"posts", "removed_by"},
	{"role_history", "new_role"},
	{"trust_penalties", "moderation_action_id"},
	{"moderation_reliefs", "relief_type"},
	{"users", "residency_claim"},
	{"proposals", "status"},
	{"invites", "token_hash"},
}

func assertColumnsInPublic(t *testing.T, pool *pgxpool.Pool, when string) {
	t.Helper()
	ctx := context.Background()

	for _, c := range publicSchemaColumns {
		var schema string
		err := pool.QueryRow(ctx,
			`SELECT table_schema FROM information_schema.columns
			 WHERE table_name = $1 AND column_name = $2`,
			c.table, c.column,
		).Scan(&schema)
		if err != nil {
			t.Errorf("%s: looking up %s.%s: %v", when, c.table, c.column, err)
			continue
		}
		if schema != "public" {
			t.Errorf("%s: %s.%s is in schema %q, want public", when, c.table, c.column, schema)
		}
	}
}

// Every -- +goose Down block in this repo was unexecuted code: RunMigrations
// only calls provider.Up, so nothing had ever run one. A rollback is the worst
// possible time to discover that.
//
// The specific hazard is the one CLAUDE.md documents for the way up. Migrations
// 00001 and 00007 switch search_path to ag_catalog for their AGE work; on the
// way up both switch it back to public-first, because DDL that runs afterwards
// otherwise creates objects in ag_catalog. Down blocks run in reverse, so
// 00007's Down runs before 00006's and 00002's — and if it leaves search_path
// pointing at ag_catalog, everything after it is exposed to the same hazard.
func TestMigrations_FullDownAndUpRoundTrip(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()

	provider, _ := testProvider(t, pool)

	// testsupport hands out an already-migrated database.
	version, err := provider.GetDBVersion(ctx)
	if err != nil {
		t.Fatalf("reading initial version: %v", err)
	}
	if version != latestVersion {
		t.Fatalf("initial version = %d, want %d", version, latestVersion)
	}
	assertColumnsInPublic(t, pool, "before the rollback")

	// All the way down. This is the first time these blocks have ever run.
	if _, err := provider.DownTo(ctx, 0); err != nil {
		t.Fatalf("rolling every migration back: %v", err)
	}
	if version, err = provider.GetDBVersion(ctx); err != nil {
		t.Fatalf("reading version after rollback: %v", err)
	}
	if version != 0 {
		t.Fatalf("version after full rollback = %d, want 0", version)
	}

	// The application's tables are gone. goose_db_version stays; it is goose's.
	for _, table := range []string{"users", "posts", "reports", "trust_penalties", "role_history", "moderation_reliefs", "proposals"} {
		var exists bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables
			 WHERE table_name = $1 AND table_schema NOT IN ('pg_catalog', 'information_schema'))`,
			table,
		).Scan(&exists); err != nil {
			t.Fatalf("checking for %s: %v", table, err)
		}
		if exists {
			t.Errorf("table %s survived a full rollback", table)
		}
	}

	// And all the way back up.
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("re-applying every migration: %v", err)
	}
	if version, err = provider.GetDBVersion(ctx); err != nil {
		t.Fatalf("reading version after re-apply: %v", err)
	}
	if version != latestVersion {
		t.Fatalf("version after round trip = %d, want %d", version, latestVersion)
	}

	assertColumnsInPublic(t, pool, "after the round trip")
}

// The failure this round trip exists to catch, asserted directly: nothing the
// migrations create may end up in ag_catalog. A column landing there still
// answers information_schema queries, so the per-column check above would pass
// on a database the application cannot actually use.
func TestMigrations_RoundTripLeavesNothingInAgCatalog(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()

	provider, _ := testProvider(t, pool)

	if _, err := provider.DownTo(ctx, 0); err != nil {
		t.Fatalf("rolling every migration back: %v", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("re-applying every migration: %v", err)
	}

	rows, err := pool.Query(ctx,
		`SELECT table_name FROM information_schema.tables
		 WHERE table_schema = 'ag_catalog'
		   AND table_name IN (
		     'users', 'posts', 'reactions', 'reports', 'vouches', 'town_config',
		     'council_votes', 'role_history', 'moderation_actions',
		     'trust_penalties', 'proposals', 'goose_db_version'
		   )`)
	if err != nil {
		t.Fatalf("querying ag_catalog: %v", err)
	}
	defer rows.Close()

	var strays []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scanning ag_catalog table name: %v", err)
		}
		strays = append(strays, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating ag_catalog tables: %v", err)
	}

	if len(strays) > 0 {
		t.Errorf("a round trip left application tables in ag_catalog: %v — "+
			"a Down block reset search_path to ag_catalog and never restored it", strays)
	}
}

// 00015's Down is the only destructive one in the repo: it DELETEs penalties
// with no moderation action before restoring NOT NULL, because those rows
// cannot satisfy the restored constraint. Running it against an empty table
// proves nothing — the DELETE would be a no-op and the ALTER would succeed
// whether or not the DELETE was there at all. So a row that the constraint
// would reject is seeded first.
func TestMigrations_Down15DeletesPenaltiesWithNoAction(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()

	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rollback-penalty"), domain.RoleMember, 50)

	// hop_depth 0, because 00016's CHECK rejects a propagated penalty with no
	// action. This is exactly the shape of a vouch revocation penalty, which is
	// what 00015 relaxed the NOT NULL for.
	_, err := pool.Exec(ctx,
		`INSERT INTO trust_penalties (id, user_id, moderation_action_id, penalty_amount, hop_depth, created_at)
		 VALUES ($1, $2, NULL, $3, 0, NOW())`,
		"penalty-no-action", user.ID, 5.0)
	if err != nil {
		t.Fatalf("seeding a penalty with no moderation action: %v", err)
	}

	// Also seed one WITH an action, which must survive the rollback — otherwise
	// a Down that deleted everything would pass just as well.
	if _, err := pool.Exec(ctx,
		`INSERT INTO moderation_actions (id, target_user_id, moderator_id, action_type, severity, reason, created_at)
		 VALUES ($1, $2, $2, 'warn', 1, 'test', NOW())`,
		"action-1", user.ID); err != nil {
		t.Fatalf("seeding a moderation action: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO trust_penalties (id, user_id, moderation_action_id, penalty_amount, hop_depth, created_at)
		 VALUES ($1, $2, $3, $4, 0, NOW())`,
		"penalty-with-action", user.ID, "action-1", 5.0); err != nil {
		t.Fatalf("seeding a penalty with a moderation action: %v", err)
	}

	provider, _ := testProvider(t, pool)

	// Down to 14, so 00017, 00016 and 00015's Down blocks run and stop there —
	// trust_penalties still exists and can be inspected.
	if _, err := provider.DownTo(ctx, 14); err != nil {
		t.Fatalf("rolling back to 00014: %v", err)
	}

	var orphaned, attached int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FILTER (WHERE moderation_action_id IS NULL),
		        COUNT(*) FILTER (WHERE moderation_action_id IS NOT NULL)
		 FROM trust_penalties`).Scan(&orphaned, &attached); err != nil {
		t.Fatalf("counting penalties after rollback: %v", err)
	}
	if orphaned != 0 {
		t.Errorf("%d penalties with no moderation action survived 00015's Down", orphaned)
	}
	if attached != 1 {
		t.Errorf("penalties with an action = %d, want the seeded one left alone", attached)
	}

	// The column is NOT NULL again, which is the state 00015 relaxed.
	var isNullable string
	if err := pool.QueryRow(ctx,
		`SELECT is_nullable FROM information_schema.columns
		 WHERE table_schema = 'public' AND table_name = 'trust_penalties'
		   AND column_name = 'moderation_action_id'`).Scan(&isNullable); err != nil {
		t.Fatalf("reading moderation_action_id nullability: %v", err)
	}
	if isNullable != "NO" {
		t.Errorf("moderation_action_id is_nullable = %q, want NO after rolling 00015 back", isNullable)
	}

	// Forward again, so the rollback is not a one-way door.
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("re-applying after the partial rollback: %v", err)
	}
	assertColumnsInPublic(t, pool, "after rolling 00015 back and forward")
}

// 00017 is the newest migration and its Down is the newest untested block.
func TestMigrations_Down17DropsRemovedBy(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()

	provider, _ := testProvider(t, pool)

	if _, err := provider.DownTo(ctx, 16); err != nil {
		t.Fatalf("rolling 00017 back: %v", err)
	}

	var exists bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns
		 WHERE table_name = 'posts' AND column_name = 'removed_by')`).Scan(&exists); err != nil {
		t.Fatalf("checking for posts.removed_by: %v", err)
	}
	if exists {
		t.Error("posts.removed_by survived 00017's Down")
	}

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("re-applying 00017: %v", err)
	}
	assertColumnsInPublic(t, pool, "after rolling 00017 back and forward")
}

// 00007's Down must restore search_path the way its Up does.
//
// The Up block switches to ag_catalog for its AGE call and then switches back
// to public-first, because CLAUDE.md's hazard is that DDL running afterwards
// otherwise creates objects in ag_catalog. The Down block made the same switch
// and never switched back, so after a rollback past 00007 the connection was
// left pointing at ag_catalog — and `SET search_path` is session state that
// survives the migration that set it.
//
// The assertion is behavioural rather than a SHOW, because the string is not
// the problem: where a subsequent CREATE actually lands is. Before the fix this
// table was created in ag_catalog.
//
// It is reachable in practice. The full round trip happens to survive only
// because 00007's *Up* re-runs on the way back and resets the path before any
// table is created; nothing else was protecting it, and anything the caller
// does on that connection after a rollback has no such protection.
func TestMigrations_Down7RestoresSearchPath(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()

	provider, db := testProvider(t, pool)

	// Down to 6, so 00007's Down is the last block that ran.
	if _, err := provider.DownTo(ctx, 6); err != nil {
		t.Fatalf("rolling back past 00007: %v", err)
	}

	// Same connection the rollback used — that is the point.
	if _, err := db.ExecContext(ctx, `CREATE TABLE search_path_probe (id int)`); err != nil {
		t.Fatalf("creating a probe table after the rollback: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS public.search_path_probe`)
		_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS ag_catalog.search_path_probe`)
	})

	var schema string
	if err := pool.QueryRow(ctx,
		`SELECT table_schema FROM information_schema.tables WHERE table_name = 'search_path_probe'`,
	).Scan(&schema); err != nil {
		t.Fatalf("looking up the probe table: %v", err)
	}

	if schema != "public" {
		t.Errorf("after rolling 00007 back, a new table lands in schema %q, want public — "+
			"the Down block left search_path pointing at ag_catalog and never reset it", schema)
	}
}

// 00021's Up is the second destructive block in the repo: it DELETEs
// council_votes whose proposal_id refers to no proposal, because until that
// migration there was no proposals table for any of them to refer to and the
// foreign key it adds could not otherwise be created.
//
// Running it against an empty table would prove nothing — the DELETE would be a
// no-op and the ALTER would succeed with or without it — so an orphaned vote of
// exactly the shape the old shell produced is seeded first, from below the
// migration where the foreign key does not yet exist to prevent it.
func TestMigrations_Up21DeletesOrphanedCouncilVotes(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()

	voter := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("orphan-vote"), domain.RoleCouncil, 100)

	provider, _ := testProvider(t, pool)

	// Down to 20: proposals and the foreign key are gone, which is the state
	// every existing deployment is in.
	if _, err := provider.DownTo(ctx, 20); err != nil {
		t.Fatalf("rolling back to 00020: %v", err)
	}

	// A vote on a proposal id that refers to nothing — the only kind of vote
	// this table has ever held.
	if _, err := pool.Exec(ctx,
		`INSERT INTO council_votes (id, proposal_id, voter_id, vote, created_at)
		 VALUES ($1, $2, $3, 'approve', NOW())`,
		"vote-orphan", "a-proposal-that-never-existed", voter.ID); err != nil {
		t.Fatalf("seeding an orphaned council vote: %v", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("re-applying 00021: %v", err)
	}

	var remaining int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM council_votes WHERE id = 'vote-orphan'`).Scan(&remaining); err != nil {
		t.Fatalf("counting the orphaned vote: %v", err)
	}
	if remaining != 0 {
		t.Errorf("the orphaned council vote survived 00021: %d rows, want 0", remaining)
	}

	// And the constraint is real, not merely declared: a fresh orphan must now
	// be refused rather than silently accumulating for the next migration to
	// clean up.
	_, err := pool.Exec(ctx,
		`INSERT INTO council_votes (id, proposal_id, voter_id, vote, created_at)
		 VALUES ($1, $2, $3, 'approve', NOW())`,
		"vote-orphan-2", "still-not-a-proposal", voter.ID)
	if err == nil {
		t.Error("council_votes accepted a vote on a proposal that does not exist; " +
			"the foreign key from 00021 is missing")
	}
}

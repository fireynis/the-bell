//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/fireynis/the-bell/internal/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InTx is the primitive the bootstrap wizard trusts to create the first council
// members and the town config as one unit. Its commit and rollback behaviour is
// a property of Postgres, not of the Go code, so it is exercised against a real
// database: a fake would only prove the fake commits.

func txUser(kratosID string) *domain.User {
	now := time.Now()
	return &domain.User{
		ID:               "tx-user-" + kratosID,
		KratosIdentityID: kratosID,
		DisplayName:      "Tx User",
		TrustScore:       50,
		Role:             domain.RoleMember,
		IsActive:         true,
		JoinedAt:         now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

// countUsers and readConfig read through a fresh pool connection rather than
// through the transaction, so they see only what was actually committed.
func countUsers(t *testing.T, pool *pgxpool.Pool, id string) int {
	t.Helper()

	var n int
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM users WHERE id = $1", id).Scan(&n); err != nil {
		t.Fatalf("counting users: %v", err)
	}
	return n
}

func readConfig(t *testing.T, pool *pgxpool.Pool, key string) (string, bool) {
	t.Helper()

	var value string
	err := pool.QueryRow(context.Background(), "SELECT value FROM town_config WHERE key = $1", key).Scan(&value)
	if err != nil {
		return "", false
	}
	return value, true
}

func TestTransactor_InTx_CommitsOnSuccess(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	tx := postgres.NewTransactor(pool)

	kratosID := testsupport.UniqueKratosID("commit")
	user := txUser(kratosID)

	err := tx.InTx(ctx, func(repos service.RepoSet) error {
		users := repos.Users()
		config := repos.Config()

		if err := users.CreateUser(ctx, user); err != nil {
			return err
		}
		return config.SetTownConfig(ctx, "town_name", "Bellwether")
	})
	if err != nil {
		t.Fatalf("InTx: %v", err)
	}

	if got := countUsers(t, pool, user.ID); got != 1 {
		t.Errorf("user rows = %d, want 1 after a successful commit", got)
	}
	if value, ok := readConfig(t, pool, "town_name"); !ok || value != "Bellwether" {
		t.Errorf("town_name = %q (present=%v), want %q", value, ok, "Bellwether")
	}
}

// The whole point of the transaction: a failure partway through must not leave
// the earlier writes behind. A half-configured town is worse than no town.
func TestTransactor_InTx_RollsBackEveryWriteOnError(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	tx := postgres.NewTransactor(pool)

	wantErr := errors.New("second council member is not a valid email")
	user := txUser(testsupport.UniqueKratosID("rollback"))

	err := tx.InTx(ctx, func(repos service.RepoSet) error {
		users := repos.Users()
		config := repos.Config()

		// Three successful writes, then a failure.
		if err := users.CreateUser(ctx, user); err != nil {
			return err
		}
		if err := config.SetTownConfig(ctx, "town_name", "Bellwether"); err != nil {
			return err
		}
		if err := config.SetTownConfig(ctx, "setup_complete", "true"); err != nil {
			return err
		}
		return wantErr
	})

	if !errors.Is(err, wantErr) {
		t.Fatalf("InTx error = %v, want the closure's error returned unchanged", err)
	}

	if got := countUsers(t, pool, user.ID); got != 0 {
		t.Errorf("user rows = %d, want 0 — the user was committed despite the error", got)
	}
	for _, key := range []string{"town_name", "setup_complete"} {
		if value, ok := readConfig(t, pool, key); ok {
			t.Errorf("config %q = %q, want it rolled back", key, value)
		}
	}
}

// A panic unwinds through InTx's deferred Rollback. Nothing may be committed,
// and — the part that would be invisible until the pool ran dry — the
// connection must go back to the pool rather than being held by an open
// transaction.
func TestTransactor_InTx_PanicRollsBackAndReleasesTheConnection(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	tx := postgres.NewTransactor(pool)

	user := txUser(testsupport.UniqueKratosID("panic"))

	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected the panic to propagate out of InTx")
			}
		}()

		_ = tx.InTx(ctx, func(repos service.RepoSet) error {
			users := repos.Users()

			if err := users.CreateUser(ctx, user); err != nil {
				return err
			}
			panic("bootstrap wizard blew up mid-transaction")
		})
	}()

	if got := countUsers(t, pool, user.ID); got != 0 {
		t.Errorf("user rows = %d, want 0 — the panic left the write committed", got)
	}

	// The database is still usable: had the transaction been left open, the
	// row it inserted would still hold a lock and this write would block.
	after := txUser(testsupport.UniqueKratosID("after-panic"))
	if err := tx.InTx(ctx, func(repos service.RepoSet) error {
		users := repos.Users()

		return users.CreateUser(ctx, after)
	}); err != nil {
		t.Fatalf("transactor unusable after a panic: %v", err)
	}
	if got := countUsers(t, pool, after.ID); got != 1 {
		t.Errorf("follow-up user rows = %d, want 1", got)
	}
}

// A constraint violation inside the closure is an ordinary error return, and it
// must roll back what came before it just like any other failure.
func TestTransactor_InTx_RollsBackOnConstraintViolation(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	tx := postgres.NewTransactor(pool)

	kratosID := testsupport.UniqueKratosID("dup")
	first := txUser(kratosID)
	if err := tx.InTx(ctx, func(repos service.RepoSet) error {
		users := repos.Users()

		return users.CreateUser(ctx, first)
	}); err != nil {
		t.Fatalf("seeding first user: %v", err)
	}

	// Same Kratos identity, different primary key: the unique index rejects it.
	duplicate := txUser(kratosID)
	duplicate.ID = first.ID + "-second"

	err := tx.InTx(ctx, func(repos service.RepoSet) error {
		users := repos.Users()
		config := repos.Config()

		if err := config.SetTownConfig(ctx, "half_written", "yes"); err != nil {
			return err
		}
		return users.CreateUser(ctx, duplicate)
	})
	if err == nil {
		t.Fatal("InTx succeeded despite a duplicate kratos_identity_id")
	}

	if value, ok := readConfig(t, pool, "half_written"); ok {
		t.Errorf("config half_written = %q, want it rolled back with the failed insert", value)
	}
	if got := countUsers(t, pool, duplicate.ID); got != 0 {
		t.Errorf("duplicate user rows = %d, want 0", got)
	}
	if got := countUsers(t, pool, first.ID); got != 1 {
		t.Errorf("first user rows = %d, want it left intact at 1", got)
	}
}

// Repositories handed to the closure are transaction-scoped: a read inside the
// transaction sees the transaction's own uncommitted writes.
func TestTransactor_InTx_ReadsSeeUncommittedWrites(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	tx := postgres.NewTransactor(pool)

	err := tx.InTx(ctx, func(repos service.RepoSet) error {
		config := repos.Config()

		if err := config.SetTownConfig(ctx, "town_name", "Bellwether"); err != nil {
			return err
		}

		value, err := config.GetTownConfig(ctx, "town_name")
		if err != nil {
			return err
		}
		if value != "Bellwether" {
			t.Errorf("in-transaction read = %q, want %q", value, "Bellwether")
		}

		// Meanwhile the value is not yet visible outside the transaction.
		if outside, ok := readConfig(t, pool, "town_name"); ok {
			t.Errorf("uncommitted value %q was visible outside the transaction", outside)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("InTx: %v", err)
	}
}

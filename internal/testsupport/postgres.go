package testsupport

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"testing"

	"github.com/fireynis/the-bell/internal/database"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// adminDBName is the database created by the container entrypoint. It is
	// never migrated; it exists so we always have somewhere to connect while
	// issuing CREATE DATABASE / DROP DATABASE against the others.
	adminDBName = "thebell_admin"
	adminDBUser = "testuser"
	adminDBPass = "testpass"

	// templateDBName holds one migrated schema that every per-test database is
	// cloned from. Cloning a template is far cheaper than re-running the full
	// migration set for each test.
	templateDBName = "thebell_template"
)

var (
	pgOnce       once
	pgContainer  *tcpostgres.PostgresContainer
	pgAdminPool  *pgxpool.Pool
	pgBaseDSN    string
	templateOnce once

	// createMu serialises CREATE DATABASE ... TEMPLATE. Postgres rejects a
	// clone while another session is connected to the source, so two concurrent
	// clones of the same template can collide.
	createMu sync.Mutex
)

// TestDB returns a pool connected to a freshly-migrated, uniquely-named
// database. Every call gets its own database, so tests never see each other's
// rows, but they all share a single Postgres container per test binary.
//
// The database is dropped and the pool closed when the test completes.
func TestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	if err := ensureTemplate(ctx); err != nil {
		t.Fatalf("preparing postgres test template: %v", err)
	}

	name := fmt.Sprintf("bell_test_%d", dbCounter.Add(1))

	createMu.Lock()
	_, err := pgAdminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %q TEMPLATE %q", name, templateDBName))
	createMu.Unlock()
	if err != nil {
		t.Fatalf("creating test database %s: %v", name, err)
	}

	pool, err := database.Connect(ctx, dsnForDB(name))
	if err != nil {
		t.Fatalf("connecting to test database %s: %v", name, err)
	}

	t.Cleanup(func() {
		pool.Close()
		// FORCE terminates any connection the test left behind; without it a
		// leaked pool would keep the drop from succeeding.
		if _, err := pgAdminPool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %q WITH (FORCE)", name)); err != nil {
			t.Logf("dropping test database %s: %v", name, err)
		}
	})

	return pool
}

// ensurePostgres starts the shared Postgres+AGE container once per test binary.
func ensurePostgres(ctx context.Context) error {
	return pgOnce.do(func() error {
		container, err := tcpostgres.Run(ctx,
			postgresImage,
			tcpostgres.WithDatabase(adminDBName),
			tcpostgres.WithUsername(adminDBUser),
			tcpostgres.WithPassword(adminDBPass),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).
					WithStartupTimeout(startupTimeout),
			),
		)
		if err != nil {
			return fmt.Errorf("starting postgres container: %w", err)
		}
		pgContainer = container

		dsn, err := container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			return fmt.Errorf("getting connection string: %w", err)
		}
		pgBaseDSN = dsn

		pool, err := database.Connect(ctx, dsn)
		if err != nil {
			return fmt.Errorf("connecting to admin database: %w", err)
		}
		pgAdminPool = pool

		return nil
	})
}

// ensureTemplate creates and migrates the template database once per test
// binary. The migration pool is closed afterwards: Postgres refuses to clone a
// database that still has connections.
func ensureTemplate(ctx context.Context) error {
	if err := ensurePostgres(ctx); err != nil {
		return err
	}

	return templateOnce.do(func() error {
		if _, err := pgAdminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %q", templateDBName)); err != nil {
			return fmt.Errorf("creating template database: %w", err)
		}

		pool, err := database.Connect(ctx, dsnForDB(templateDBName))
		if err != nil {
			return fmt.Errorf("connecting to template database: %w", err)
		}
		defer pool.Close()

		// The apache/age image ships AGE pre-installed; migration 00001 creates
		// the extension and 00007 creates the trust graph. Both are carried into
		// every clone of this template.
		if err := database.RunMigrations(ctx, pool); err != nil {
			return fmt.Errorf("migrating template database: %w", err)
		}
		return nil
	})
}

// dsnForDB rewrites the container DSN to point at a different database on the
// same server.
func dsnForDB(name string) string {
	u, err := url.Parse(pgBaseDSN)
	if err != nil {
		// pgBaseDSN comes from Testcontainers and is always a valid URL.
		panic(fmt.Sprintf("testsupport: unparseable container DSN %q: %v", pgBaseDSN, err))
	}
	u.Path = "/" + name
	return u.String()
}

// stopPostgres tears down the shared Postgres container, if one was started.
func stopPostgres(ctx context.Context) {
	if pgAdminPool != nil {
		pgAdminPool.Close()
		pgAdminPool = nil
	}
	if pgContainer != nil {
		_ = pgContainer.Terminate(ctx)
		pgContainer = nil
	}
}

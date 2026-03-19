# Vouch Repository Queries Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create the data access layer for vouches: a relational `vouches` table with sqlc CRUD queries, plus hand-written Apache AGE Cypher queries for graph operations (add/remove edges, traversal, cycle detection).

**Architecture:** Two-layer approach. Standard sqlc-generated code handles CRUD on the `vouches` table (relational). A hand-written `AGEQuerier` struct in the same package wraps AGE Cypher queries for graph operations on `trust_graph`. Each AGE query runs inside a transaction that first loads the AGE extension (`LOAD 'age'`). Parameters are passed as JSON→agtype to avoid SQL injection.

**Tech Stack:** PostgreSQL, Apache AGE (graph extension), sqlc (code generation), pgx/v5 (driver), goose (migrations)

**Beads issue:** `the-bell-8ee.1`

---

### Task 1: Create the vouches migration

**Files:**
- Create: `migrations/00008_create_vouches.sql`

**Step 1: Write the migration file**

```sql
-- +goose Up
CREATE TABLE vouches (
    id          TEXT PRIMARY KEY,
    voucher_id  TEXT NOT NULL REFERENCES users(id),
    vouchee_id  TEXT NOT NULL REFERENCES users(id),
    status      TEXT NOT NULL DEFAULT 'active',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at  TIMESTAMPTZ,
    CONSTRAINT uq_voucher_vouchee UNIQUE (voucher_id, vouchee_id),
    CONSTRAINT chk_no_self_vouch CHECK (voucher_id != vouchee_id)
);

CREATE INDEX idx_vouches_vouchee_status ON vouches(vouchee_id, status);
CREATE INDEX idx_vouches_voucher_status ON vouches(voucher_id, status);

-- +goose Down
DROP TABLE IF EXISTS vouches;
```

Design decisions:
- `UNIQUE (voucher_id, vouchee_id)` — one vouch record per pair, status toggles between active/revoked
- `CHECK (voucher_id != vouchee_id)` — DB-level guard against self-vouching
- Indexes on `(vouchee_id, status)` and `(voucher_id, status)` for the common query patterns (list active vouches for/by a user)
- Foreign keys to `users(id)` for referential integrity

**Step 2: Verify build still passes**

Run: `cd /home/jeremy/services/the-bell && go build ./...`
Expected: success (migration is just SQL, no Go changes yet)

**Step 3: Commit**

```bash
git add migrations/00008_create_vouches.sql
git commit -m "feat: add vouches table migration with self-vouch constraint"
```

---

### Task 2: Write sqlc vouch queries and regenerate

**Files:**
- Create: `queries/vouches.sql`
- Regenerate: `internal/repository/postgres/` (sqlc output)

**Step 1: Write the sqlc query file**

```sql
-- name: CreateVouch :one
INSERT INTO vouches (id, voucher_id, vouchee_id, status, created_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetVouchByID :one
SELECT * FROM vouches WHERE id = $1;

-- name: GetVouchByPair :one
SELECT * FROM vouches WHERE voucher_id = $1 AND vouchee_id = $2;

-- name: ListActiveVouchesByVouchee :many
SELECT * FROM vouches
WHERE vouchee_id = $1 AND status = 'active'
ORDER BY created_at;

-- name: ListActiveVouchesByVoucher :many
SELECT * FROM vouches
WHERE voucher_id = $1 AND status = 'active'
ORDER BY created_at;

-- name: RevokeVouch :one
UPDATE vouches
SET status = 'revoked', revoked_at = NOW()
WHERE id = $1 AND status = 'active'
RETURNING *;

-- name: CountVouchesByVoucherSince :one
SELECT COUNT(*) FROM vouches
WHERE voucher_id = $1 AND created_at >= $2;
```

Query rationale:
- `GetVouchByPair` — needed by the vouch service to check if a vouch already exists
- `ListActiveVouchesByVouchee` — for trust score calculation (who vouched for this user)
- `ListActiveVouchesByVoucher` — for the UI (who did I vouch for)
- `RevokeVouch` — only revokes active vouches, returns updated row
- `CountVouchesByVoucherSince` — for enforcing the daily vouch limit (3/day)

**Step 2: Run sqlc generate**

Run: `cd /home/jeremy/services/the-bell && sqlc generate`
Expected: generates `internal/repository/postgres/vouches.sql.go` and updates `models.go` with a `Vouch` struct

**Step 3: Verify build passes**

Run: `cd /home/jeremy/services/the-bell && go build ./...`
Expected: success

**Step 4: Verify generated code looks correct**

Read `internal/repository/postgres/vouches.sql.go` and `internal/repository/postgres/models.go`.
Expected: `Vouch` model with fields matching the table (id, voucher_id, vouchee_id, status, created_at, revoked_at). Query functions generated for all 7 queries.

**Step 5: Commit**

```bash
git add queries/vouches.sql internal/repository/postgres/
git commit -m "feat: add sqlc vouch queries with CRUD and daily limit count"
```

---

### Task 3: Write the failing AGE querier test

**Files:**
- Create: `internal/repository/postgres/age_test.go`

**Step 1: Write tests for the AGE querier**

The AGE querier wraps raw Cypher queries. We test:
1. `NewAGEQuerier` construction
2. Error propagation from the transaction layer (Begin fails, Exec fails)
3. The `withAGE` helper calls LOAD and SET correctly

```go
package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// mockTx implements a minimal pgx.Tx for testing AGE query error paths.
type mockTx struct {
	pgx.Tx
	execCalls []string
	execErr   error
	queryErr  error
	committed bool
}

func (m *mockTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	m.execCalls = append(m.execCalls, sql)
	return pgconn.CommandTag{}, m.execErr
}

func (m *mockTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, m.queryErr
}

func (m *mockTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return &mockRow{err: m.queryErr}
}

func (m *mockTx) Commit(ctx context.Context) error   { m.committed = true; return nil }
func (m *mockTx) Rollback(ctx context.Context) error  { return nil }

type mockRow struct {
	err error
}

func (r *mockRow) Scan(dest ...any) error { return r.err }

type mockBeginner struct {
	tx       *mockTx
	beginErr error
}

func (m *mockBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	if m.beginErr != nil {
		return nil, m.beginErr
	}
	return m.tx, nil
}

func TestNewAGEQuerier(t *testing.T) {
	b := &mockBeginner{tx: &mockTx{}}
	q := postgres.NewAGEQuerier(b)
	if q == nil {
		t.Fatal("expected non-nil AGEQuerier")
	}
}

func TestAddVouchEdge_BeginError(t *testing.T) {
	wantErr := errors.New("begin failed")
	b := &mockBeginner{beginErr: wantErr}
	q := postgres.NewAGEQuerier(b)

	err := q.AddVouchEdge(context.Background(), "a", "b")
	if !errors.Is(err, wantErr) {
		t.Fatalf("got %v, want %v", err, wantErr)
	}
}

func TestAddVouchEdge_LoadsAGE(t *testing.T) {
	tx := &mockTx{}
	b := &mockBeginner{tx: tx}
	q := postgres.NewAGEQuerier(b)

	// Will fail at the Cypher query step, but LOAD/SET should be called first
	tx.queryErr = errors.New("cypher not available")
	_ = q.AddVouchEdge(context.Background(), "voucher-1", "vouchee-1")

	if len(tx.execCalls) < 2 {
		t.Fatalf("expected at least 2 exec calls (LOAD + SET), got %d", len(tx.execCalls))
	}
	if tx.execCalls[0] != "LOAD 'age'" {
		t.Errorf("first exec = %q, want LOAD 'age'", tx.execCalls[0])
	}
	wantSet := `SET search_path = ag_catalog, "$user", public`
	if tx.execCalls[1] != wantSet {
		t.Errorf("second exec = %q, want %q", tx.execCalls[1], wantSet)
	}
}

func TestRemoveVouchEdge_BeginError(t *testing.T) {
	wantErr := errors.New("begin failed")
	b := &mockBeginner{beginErr: wantErr}
	q := postgres.NewAGEQuerier(b)

	err := q.RemoveVouchEdge(context.Background(), "a", "b")
	if !errors.Is(err, wantErr) {
		t.Fatalf("got %v, want %v", err, wantErr)
	}
}

func TestFindVouchersUpToDepth_BeginError(t *testing.T) {
	wantErr := errors.New("begin failed")
	b := &mockBeginner{beginErr: wantErr}
	q := postgres.NewAGEQuerier(b)

	_, err := q.FindVouchersUpToDepth(context.Background(), "u1", 3)
	if !errors.Is(err, wantErr) {
		t.Fatalf("got %v, want %v", err, wantErr)
	}
}

func TestFindVouchersUpToDepth_InvalidDepth(t *testing.T) {
	b := &mockBeginner{tx: &mockTx{}}
	q := postgres.NewAGEQuerier(b)

	_, err := q.FindVouchersUpToDepth(context.Background(), "u1", 0)
	if err == nil {
		t.Fatal("expected error for depth <= 0")
	}
	_, err = q.FindVouchersUpToDepth(context.Background(), "u1", -1)
	if err == nil {
		t.Fatal("expected error for negative depth")
	}
}

func TestHasCyclicVouch_BeginError(t *testing.T) {
	wantErr := errors.New("begin failed")
	b := &mockBeginner{beginErr: wantErr}
	q := postgres.NewAGEQuerier(b)

	_, err := q.HasCyclicVouch(context.Background(), "a", "b")
	if !errors.Is(err, wantErr) {
		t.Fatalf("got %v, want %v", err, wantErr)
	}
}
```

**Step 2: Run the tests — expect failure**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/repository/postgres/ -v -run TestNewAGE`
Expected: compilation error — `postgres.NewAGEQuerier` undefined

---

### Task 4: Implement the AGE querier

**Files:**
- Create: `internal/repository/postgres/age.go`

**Step 1: Write the AGE querier implementation**

```go
package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Beginner provides Begin for AGE queries that need per-transaction session setup.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// AGEQuerier executes Apache AGE Cypher queries against the trust_graph.
type AGEQuerier struct {
	db Beginner
}

// NewAGEQuerier creates an AGEQuerier backed by the given transaction starter
// (typically a *pgxpool.Pool).
func NewAGEQuerier(db Beginner) *AGEQuerier {
	return &AGEQuerier{db: db}
}

// withAGE runs fn inside a transaction after loading the AGE extension and
// setting the search path. This is required because LOAD 'age' is session-scoped
// and pgxpool may hand out any connection.
func (q *AGEQuerier) withAGE(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := q.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "LOAD 'age'"); err != nil {
		return fmt.Errorf("loading age extension: %w", err)
	}
	if _, err := tx.Exec(ctx, `SET search_path = ag_catalog, "$user", public`); err != nil {
		return fmt.Errorf("setting search path: %w", err)
	}

	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// cypherParams builds a JSON string from key-value pairs for use as AGE Cypher
// parameters. Keys must be strings; values are JSON-encoded.
func cypherParams(kvs ...any) (string, error) {
	if len(kvs)%2 != 0 {
		return "", fmt.Errorf("cypherParams requires even number of arguments")
	}
	m := make(map[string]any, len(kvs)/2)
	for i := 0; i < len(kvs); i += 2 {
		key, ok := kvs[i].(string)
		if !ok {
			return "", fmt.Errorf("cypherParams key at index %d is not a string", i)
		}
		m[key] = kvs[i+1]
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("marshaling cypher params: %w", err)
	}
	return string(b), nil
}

// AddVouchEdge creates a VOUCHES_FOR edge between two User vertices in the
// trust graph. Vertices are created if they don't exist (MERGE).
func (q *AGEQuerier) AddVouchEdge(ctx context.Context, voucherID, voucheeID string) error {
	return q.withAGE(ctx, func(tx pgx.Tx) error {
		params, err := cypherParams("voucher_id", voucherID, "vouchee_id", voucheeID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			SELECT * FROM cypher('trust_graph', $$
				MERGE (a:User {id: $voucher_id})
				MERGE (b:User {id: $vouchee_id})
				CREATE (a)-[:VOUCHES_FOR]->(b)
			$$, $1::agtype) AS (v agtype)
		`, params)
		if err != nil {
			return fmt.Errorf("adding vouch edge: %w", err)
		}
		return nil
	})
}

// RemoveVouchEdge deletes the VOUCHES_FOR edge between two users.
func (q *AGEQuerier) RemoveVouchEdge(ctx context.Context, voucherID, voucheeID string) error {
	return q.withAGE(ctx, func(tx pgx.Tx) error {
		params, err := cypherParams("voucher_id", voucherID, "vouchee_id", voucheeID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			SELECT * FROM cypher('trust_graph', $$
				MATCH (a:User {id: $voucher_id})-[e:VOUCHES_FOR]->(b:User {id: $vouchee_id})
				DELETE e
			$$, $1::agtype) AS (v agtype)
		`, params)
		if err != nil {
			return fmt.Errorf("removing vouch edge: %w", err)
		}
		return nil
	})
}

// FindVouchersUpToDepth returns the IDs of all users who have vouched for
// userID, traversing up to depth hops in the trust graph.
func (q *AGEQuerier) FindVouchersUpToDepth(ctx context.Context, userID string, depth int) ([]string, error) {
	if depth <= 0 {
		return nil, fmt.Errorf("depth must be positive, got %d", depth)
	}

	var ids []string
	err := q.withAGE(ctx, func(tx pgx.Tx) error {
		params, err := cypherParams("user_id", userID)
		if err != nil {
			return err
		}
		// depth is an int literal in the Cypher pattern — safe from injection.
		query := fmt.Sprintf(`
			SELECT * FROM cypher('trust_graph', $$
				MATCH (u:User {id: $user_id})<-[:VOUCHES_FOR*1..%d]-(v:User)
				RETURN DISTINCT v.id
			$$, $1::agtype) AS (voucher_id agtype)
		`, depth)

		rows, err := tx.Query(ctx, query, params)
		if err != nil {
			return fmt.Errorf("finding vouchers: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var raw string
			if err := rows.Scan(&raw); err != nil {
				return fmt.Errorf("scanning voucher id: %w", err)
			}
			// AGE returns agtype string values with JSON quoting: "user-123"
			ids = append(ids, strings.Trim(raw, `"`))
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// HasCyclicVouch checks whether adding a VOUCHES_FOR edge from voucherID to
// voucheeID would create a cycle in the trust graph. It returns true if a path
// already exists from voucheeID back to voucherID.
func (q *AGEQuerier) HasCyclicVouch(ctx context.Context, voucherID, voucheeID string) (bool, error) {
	var hasCycle bool
	err := q.withAGE(ctx, func(tx pgx.Tx) error {
		params, err := cypherParams("voucher_id", voucherID, "vouchee_id", voucheeID)
		if err != nil {
			return err
		}
		// Check if vouchee can already reach voucher via existing edges.
		// Upper bound of 50 prevents runaway traversals on large graphs.
		var raw string
		err = tx.QueryRow(ctx, `
			SELECT * FROM cypher('trust_graph', $$
				MATCH p = (b:User {id: $vouchee_id})-[:VOUCHES_FOR*1..50]->(a:User {id: $voucher_id})
				RETURN count(p) > 0
			$$, $1::agtype) AS (has_cycle agtype)
		`, params).Scan(&raw)
		if err != nil {
			// No rows means no path found — not a cycle
			if err.Error() == "no rows in result set" {
				hasCycle = false
				return nil
			}
			return fmt.Errorf("checking cycle: %w", err)
		}
		hasCycle = strings.TrimSpace(raw) == "true"
		return nil
	})
	return hasCycle, err
}
```

Key design decisions:
- `Beginner` interface wraps just `Begin` — satisfied by `*pgxpool.Pool`
- `withAGE` runs LOAD + SET search_path per transaction (required because pgxpool rotates connections)
- `cypherParams` builds JSON safely using `encoding/json` — no string concatenation
- `FindVouchersUpToDepth` uses `fmt.Sprintf` for the depth literal (int, injection-safe)
- `HasCyclicVouch` treats "no rows" as "no cycle" since AGE returns nothing when MATCH finds no paths
- Upper bound of 50 on cycle detection prevents runaway traversals

**Step 2: Run tests to verify they pass**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/repository/postgres/ -v -count=1`
Expected: all tests pass (constructor, Begin error propagation, LOAD/SET verification, depth validation)

**Step 3: Verify full build and existing tests**

Run: `cd /home/jeremy/services/the-bell && go build ./... && go vet ./... && go test ./... -count=1`
Expected: all pass

**Step 4: Commit**

```bash
git add internal/repository/postgres/age.go internal/repository/postgres/age_test.go
git commit -m "feat: add AGE querier for trust graph vouch operations"
```

---

### Task 5: Final verification and cleanup

**Step 1: Run full test suite**

Run: `cd /home/jeremy/services/the-bell && go test ./... -v -count=1`
Expected: all tests pass (existing + new AGE querier tests)

**Step 2: Verify all new files are committed**

Run: `git status`
Expected: clean working tree

**Step 3: Close the beads issue**

Run: `bd close the-bell-8ee.1`

---

## Edge Cases & Risks

| Risk | Mitigation |
|------|------------|
| AGE not installed in dev/test PostgreSQL | AGE querier tests use mocks; integration tests deferred to CI with AGE-enabled Postgres |
| `LOAD 'age'` performance overhead per query | Session-scoped within transaction; negligible vs. network round-trip |
| Duplicate VOUCHES_FOR edge in graph (vouch revoked+re-created) | Service layer should call `RemoveVouchEdge` before `AddVouchEdge` on re-vouch, or use MERGE for the edge |
| agtype scanning varies by AGE version | `strings.Trim` handles the common JSON-quoted format; integration tests will catch regressions |
| Variable-length path `*1..N` performance on large graphs | Depth capped at caller level (service enforces max 3 via `PropagationDepth`); cycle check bounded at 50 |
| `pgx.ErrNoRows` detection for HasCyclicVouch | Check error string since pgx may wrap; consider switching to `errors.Is(err, pgx.ErrNoRows)` if pgx version supports it |

## Test Strategy Summary

| Layer | What | How |
|-------|------|-----|
| Migration | Schema correctness | Applied by goose in dev/CI against real Postgres |
| sqlc queries | Generated code correctness | Trusted (sqlc); verified by build + downstream integration |
| AGE querier unit | Error propagation, LOAD/SET sequence, depth validation | Mock `Beginner`/`pgx.Tx` in `age_test.go` |
| AGE querier integration | Actual Cypher execution against trust_graph | Deferred to CI with AGE-enabled PostgreSQL container |
| Downstream | End-to-end vouch flow | Vouch service tests (task `the-bell-8ee.2`) with mock repository |

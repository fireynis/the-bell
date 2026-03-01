package postgres

import (
	"context"
	"encoding/json"
	"errors"
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
		if errors.Is(err, pgx.ErrNoRows) {
			hasCycle = false
			return nil
		}
		if err != nil {
			return fmt.Errorf("checking cycle: %w", err)
		}
		hasCycle = strings.TrimSpace(raw) == "true"
		return nil
	})
	return hasCycle, err
}

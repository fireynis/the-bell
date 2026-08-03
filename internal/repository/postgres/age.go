package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// maxGraphDepth is the upper bound for variable-length path traversals in AGE
// Cypher queries. This prevents runaway traversals on large graphs.
const maxGraphDepth = 50

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
	// SET LOCAL, not SET: a plain SET outlives the transaction and stays on the
	// pooled connection, so every later query on that connection would resolve
	// ag_catalog ahead of public. This is the same search_path hazard the
	// migrations guard against, and pgxpool hands the connection to anyone next.
	if _, err := tx.Exec(ctx, `SET LOCAL search_path = ag_catalog, "$user", public`); err != nil {
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

// validateGraphDepth bounds a traversal depth. An unbounded or very deep
// variable-length pattern can walk most of the graph, so callers must stay
// within maxGraphDepth.
func validateGraphDepth(name string, depth int) error {
	if depth <= 0 {
		return fmt.Errorf("%s must be positive, got %d", name, depth)
	}
	if depth > maxGraphDepth {
		return fmt.Errorf("%s %d exceeds maximum %d", name, depth, maxGraphDepth)
	}
	return nil
}

// parseAgtypeString reads a string value returned by AGE.
//
// AGE renders string values with JSON quoting ("user-123"), so the quotes are
// removed by decoding rather than trimming: trimming would also strip quotes
// that are genuinely part of the value and would leave any escape sequence
// (\" or \\) undecoded. Values that are not quoted JSON are returned as-is,
// which is what AGE does for an unquoted scalar.
func parseAgtypeString(raw string) string {
	var s string
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return raw
	}
	return s
}

// parseAgtypeInt reads an integer value returned by AGE, such as the hop count
// from min(length(p)).
func parseAgtypeInt(raw string) (int, error) {
	var n int
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &n); err != nil {
		return 0, fmt.Errorf("parsing agtype integer %q: %w", raw, err)
	}
	return n, nil
}

// parseAgtypeBool reads a boolean returned by AGE. Anything that is not
// literally true is treated as false, so an unexpected value fails closed —
// for the cycle check that means reporting no cycle rather than a phantom one.
func parseAgtypeBool(raw string) bool {
	return strings.TrimSpace(raw) == "true"
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
				MERGE (a)-[:VOUCHES_FOR]->(b)
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
	if err := validateGraphDepth("depth", depth); err != nil {
		return nil, err
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
			ids = append(ids, parseAgtypeString(raw))
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// FindVouchersWithDepth returns a map of user IDs to their minimum hop depth
// for all users who have vouched (directly or transitively) for userID, up to
// maxDepth hops. Used for penalty propagation where hop depth determines the
// decay multiplier.
func (q *AGEQuerier) FindVouchersWithDepth(ctx context.Context, userID string, maxDepth int) (map[string]int, error) {
	if err := validateGraphDepth("maxDepth", maxDepth); err != nil {
		return nil, err
	}

	result := make(map[string]int)
	err := q.withAGE(ctx, func(tx pgx.Tx) error {
		params, err := cypherParams("user_id", userID)
		if err != nil {
			return err
		}
		query := fmt.Sprintf(`
			SELECT * FROM cypher('trust_graph', $$
				MATCH p = (u:User {id: $user_id})<-[:VOUCHES_FOR*1..%d]-(v:User)
				WHERE v.id <> $user_id
				RETURN v.id, min(length(p))
			$$, $1::agtype) AS (voucher_id agtype, hop_depth agtype)
		`, maxDepth)

		rows, err := tx.Query(ctx, query, params)
		if err != nil {
			return fmt.Errorf("finding vouchers with depth: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var rawID, rawDepth string
			if err := rows.Scan(&rawID, &rawDepth); err != nil {
				return fmt.Errorf("scanning voucher depth: %w", err)
			}
			id := parseAgtypeString(rawID)
			depth, err := parseAgtypeInt(rawDepth)
			if err != nil {
				return fmt.Errorf("parsing hop depth for %s: %w", id, err)
			}
			result[id] = depth
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return result, nil
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
		var raw string
		query := fmt.Sprintf(`
			SELECT * FROM cypher('trust_graph', $$
				MATCH p = (b:User {id: $vouchee_id})-[:VOUCHES_FOR*1..%d]->(a:User {id: $voucher_id})
				RETURN count(p) > 0
			$$, $1::agtype) AS (has_cycle agtype)
		`, maxGraphDepth)
		err = tx.QueryRow(ctx, query, params).Scan(&raw)
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

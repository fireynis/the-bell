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

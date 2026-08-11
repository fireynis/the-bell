package postgres

import (
	"context"

	"github.com/fireynis/the-bell/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Transactor implements service.Transactor using pgxpool transactions.
type Transactor struct {
	pool *pgxpool.Pool
}

func NewTransactor(pool *pgxpool.Pool) *Transactor {
	return &Transactor{pool: pool}
}

// txRepos is the service.RepoSet handed to an InTx callback. Every repository
// it returns is built over the same *Queries, and therefore the same pgx.Tx, so
// a callback cannot accidentally mix a transactional write with a pooled one.
type txRepos struct {
	q *Queries
}

func (r txRepos) Users() service.UserRepository    { return NewUserRepo(r.q) }
func (r txRepos) Config() service.ConfigRepository { return NewConfigRepo(r.q) }

func (t *Transactor) InTx(ctx context.Context, fn func(repos service.RepoSet) error) error {
	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := fn(txRepos{q: New(tx)}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

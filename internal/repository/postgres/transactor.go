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

func (t *Transactor) InTx(ctx context.Context, fn func(users service.UserRepository, config service.ConfigRepository) error) error {
	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	q := New(tx)
	users := NewUserRepo(q)
	config := NewConfigRepo(q)

	if err := fn(users, config); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

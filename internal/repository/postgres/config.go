package postgres

import (
	"context"
	"errors"

	"github.com/fireynis/the-bell/internal/service"
	"github.com/jackc/pgx/v5"
)

// ConfigRepo adapts sqlc queries to the service.ConfigRepository interface.
type ConfigRepo struct {
	q *Queries
}

func NewConfigRepo(q *Queries) *ConfigRepo {
	return &ConfigRepo{q: q}
}

func (r *ConfigRepo) SetTownConfig(ctx context.Context, key, value string) error {
	return r.q.SetTownConfig(ctx, SetTownConfigParams{Key: key, Value: value})
}

func (r *ConfigRepo) GetTownConfig(ctx context.Context, key string) (string, error) {
	val, err := r.q.GetTownConfig(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", service.ErrNotFound
	}
	return val, err
}

func (r *ConfigRepo) ListTownConfig(ctx context.Context) (map[string]string, error) {
	rows, err := r.q.ListTownConfig(ctx)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(rows))
	for _, row := range rows {
		result[row.Key] = row.Value
	}
	return result, nil
}

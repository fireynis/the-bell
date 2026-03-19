package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// VouchRepo adapts sqlc queries to the service.VouchRepository interface.
type VouchRepo struct {
	q *Queries
}

func NewVouchRepo(q *Queries) *VouchRepo {
	return &VouchRepo{q: q}
}

func (r *VouchRepo) CreateVouch(ctx context.Context, vouch *domain.Vouch) error {
	_, err := r.q.CreateVouch(ctx, CreateVouchParams{
		ID:        vouch.ID,
		VoucherID: vouch.VoucherID,
		VoucheeID: vouch.VoucheeID,
		Status:    string(vouch.Status),
		CreatedAt: pgtype.Timestamptz{Time: vouch.CreatedAt, Valid: true},
	})
	return err
}

func (r *VouchRepo) GetVouchByID(ctx context.Context, id string) (*domain.Vouch, error) {
	row, err := r.q.GetVouchByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, service.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return vouchFromRow(row), nil
}

func (r *VouchRepo) GetVouchByPair(ctx context.Context, voucherID, voucheeID string) (*domain.Vouch, error) {
	row, err := r.q.GetVouchByPair(ctx, GetVouchByPairParams{
		VoucherID: voucherID,
		VoucheeID: voucheeID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, service.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return vouchFromRow(row), nil
}

func (r *VouchRepo) CountVouchesByVoucherSince(ctx context.Context, voucherID string, since time.Time) (int64, error) {
	return r.q.CountVouchesByVoucherSince(ctx, CountVouchesByVoucherSinceParams{
		VoucherID: voucherID,
		CreatedAt: pgtype.Timestamptz{Time: since, Valid: true},
	})
}

func (r *VouchRepo) ListActiveVouchesByVouchee(ctx context.Context, voucheeID string) ([]*domain.Vouch, error) {
	rows, err := r.q.ListActiveVouchesByVouchee(ctx, voucheeID)
	if err != nil {
		return nil, err
	}

	vouches := make([]*domain.Vouch, len(rows))
	for i, row := range rows {
		vouches[i] = vouchFromRow(row)
	}
	return vouches, nil
}

func (r *VouchRepo) ListActiveVouchesByVoucher(ctx context.Context, voucherID string) ([]*domain.Vouch, error) {
	rows, err := r.q.ListActiveVouchesByVoucher(ctx, voucherID)
	if err != nil {
		return nil, err
	}

	vouches := make([]*domain.Vouch, len(rows))
	for i, row := range rows {
		vouches[i] = vouchFromRow(row)
	}
	return vouches, nil
}

func (r *VouchRepo) RevokeVouch(ctx context.Context, id string) error {
	_, err := r.q.RevokeVouch(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return service.ErrNotFound
	}
	return err
}

func vouchFromRow(row Vouch) *domain.Vouch {
	v := &domain.Vouch{
		ID:        row.ID,
		VoucherID: row.VoucherID,
		VoucheeID: row.VoucheeID,
		Status:    domain.VouchStatus(row.Status),
		CreatedAt: row.CreatedAt.Time,
	}
	if row.RevokedAt.Valid {
		t := row.RevokedAt.Time
		v.RevokedAt = &t
	}
	return v
}

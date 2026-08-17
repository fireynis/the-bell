package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// InviteRepo adapts sqlc queries to the service.InviteRepository interface.
type InviteRepo struct {
	q *Queries
}

func NewInviteRepo(q *Queries) *InviteRepo {
	return &InviteRepo{q: q}
}

// CreateInvite stores a new invitation, with tokenHash already hashed by the
// service. The raw token never reaches this layer.
//
// A unique violation is the live-invite index firing: another request took this
// address between the service's liveness check and this insert. It maps to
// ErrValidation so the caller is told the address already has an invitation
// rather than being handed a 500 for losing a race.
func (r *InviteRepo) CreateInvite(ctx context.Context, invite *domain.Invite, tokenHash string) error {
	_, err := r.q.CreateInvite(ctx, CreateInviteParams{
		ID:        invite.ID,
		TokenHash: tokenHash,
		Email:     invite.Email,
		Note:      invite.Note,
		InviterID: invite.InviterID,
		CreatedAt: pgtype.Timestamptz{Time: invite.CreatedAt, Valid: true},
		ExpiresAt: pgtype.Timestamptz{Time: invite.ExpiresAt, Valid: true},
	})
	if isUniqueViolation(err) {
		return domain.ErrValidation
	}
	return err
}

// GetLiveInviteByTokenHash returns the invitation a raw token names, when it is
// still redeemable. Anything else — consumed, revoked, expired, unknown — is
// ErrNotFound, because every caller answers all four the same way.
func (r *InviteRepo) GetLiveInviteByTokenHash(ctx context.Context, tokenHash string, now time.Time) (*domain.Invite, error) {
	row, err := r.q.GetLiveInviteByTokenHash(ctx, GetLiveInviteByTokenHashParams{
		TokenHash: tokenHash,
		Now:       pgtype.Timestamptz{Time: now, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	invite := inviteFromRow(row.Invite)
	invite.InviterDisplayName = row.InviterDisplayName
	return invite, nil
}

// GetLiveInviteByEmail returns the redeemable invitation waiting for an
// address, matched case-insensitively.
func (r *InviteRepo) GetLiveInviteByEmail(ctx context.Context, email string, now time.Time) (*domain.Invite, error) {
	row, err := r.q.GetLiveInviteByEmail(ctx, GetLiveInviteByEmailParams{
		Email: email,
		Now:   pgtype.Timestamptz{Time: now, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	invite := inviteFromRow(row.Invite)
	invite.InviterDisplayName = row.InviterDisplayName
	return invite, nil
}

// GetBlockingInviteByEmail returns the unconsumed, unrevoked invitation holding
// an address, expired or not — the row the unique index would collide with.
func (r *InviteRepo) GetBlockingInviteByEmail(ctx context.Context, email string) (*domain.Invite, error) {
	row, err := r.q.GetBlockingInviteByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return inviteFromRow(row), nil
}

func (r *InviteRepo) CountConsumedInvitesByEmail(ctx context.Context, email string) (int64, error) {
	return r.q.CountConsumedInvitesByEmail(ctx, email)
}

func (r *InviteRepo) CountInvitesByInviterSince(ctx context.Context, inviterID string, since time.Time) (int64, error) {
	return r.q.CountInvitesByInviterSince(ctx, CountInvitesByInviterSinceParams{
		InviterID: inviterID,
		Since:     pgtype.Timestamptz{Time: since, Valid: true},
	})
}

// ListInvitesByInviter returns the caller's own invitations, newest first, each
// naming whoever accepted it.
func (r *InviteRepo) ListInvitesByInviter(ctx context.Context, inviterID string) ([]*domain.Invite, error) {
	rows, err := r.q.ListInvitesByInviter(ctx, inviterID)
	if err != nil {
		return nil, err
	}
	invites := make([]*domain.Invite, len(rows))
	for i, row := range rows {
		invite := inviteFromRow(row.Invite)
		if row.ConsumedByDisplayName.Valid {
			invite.ConsumedByDisplayName = row.ConsumedByDisplayName.String
		}
		invites[i] = invite
	}
	return invites, nil
}

// RevokeInvite withdraws one of the caller's own open invitations.
//
// No rows means the invitation is not the caller's, not open, or not there —
// all ErrNotFound, which is what keeps the endpoint from confirming that
// somebody else's invitation exists.
func (r *InviteRepo) RevokeInvite(ctx context.Context, id, inviterID string, now time.Time) error {
	_, err := r.q.RevokeInviteByInviter(ctx, RevokeInviteByInviterParams{
		ID:        id,
		InviterID: inviterID,
		RevokedAt: pgtype.Timestamptz{Time: now, Valid: true},
		Now:       pgtype.Timestamptz{Time: now, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

// ReapInvite frees the address an expired invitation still holds. It is a
// no-op when the row has since been accepted or withdrawn.
func (r *InviteRepo) ReapInvite(ctx context.Context, id string, now time.Time) error {
	return r.q.ReapInviteForReuse(ctx, ReapInviteForReuseParams{
		ID:        id,
		RevokedAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
}

// ConsumeInvite marks an invitation redeemed by userID, exactly once.
//
// No rows means somebody else got there first (or the invitation stopped being
// live in between), which is ErrNotFound. Redeem treats that as "nothing to do"
// rather than an error, because the invitation has been honoured either way.
func (r *InviteRepo) ConsumeInvite(ctx context.Context, id, userID string, now time.Time) (*domain.Invite, error) {
	row, err := r.q.ConsumeInvite(ctx, ConsumeInviteParams{
		ID:         id,
		ConsumedAt: pgtype.Timestamptz{Time: now, Valid: true},
		ConsumedBy: pgtype.Text{String: userID, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return inviteFromRow(row), nil
}

// inviteFromRow converts a stored row, dropping token_hash: domain.Invite has
// no field for it, which is what stops the hash from travelling any further
// than this package.
func inviteFromRow(row Invite) *domain.Invite {
	invite := &domain.Invite{
		ID:        row.ID,
		Email:     row.Email,
		Note:      row.Note,
		InviterID: row.InviterID,
		CreatedAt: row.CreatedAt.Time,
		ExpiresAt: row.ExpiresAt.Time,
	}
	if row.ConsumedAt.Valid {
		t := row.ConsumedAt.Time
		invite.ConsumedAt = &t
	}
	if row.ConsumedBy.Valid {
		invite.ConsumedBy = row.ConsumedBy.String
	}
	if row.RevokedAt.Valid {
		t := row.RevokedAt.Time
		invite.RevokedAt = &t
	}
	return invite
}

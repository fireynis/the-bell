package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// UserRepo adapts sqlc queries to the service.UserRepository interface.
type UserRepo struct {
	q *Queries
}

func NewUserRepo(q *Queries) *UserRepo {
	return &UserRepo{q: q}
}

func (r *UserRepo) CreateUser(ctx context.Context, user *domain.User) error {
	_, err := r.q.CreateUser(ctx, CreateUserParams{
		ID:               user.ID,
		KratosIdentityID: user.KratosIdentityID,
		DisplayName:      user.DisplayName,
		Bio:              user.Bio,
		AvatarUrl:        user.AvatarURL,
		TrustScore:       user.TrustScore,
		Role:             string(user.Role),
		IsActive:         user.IsActive,
		JoinedAt:         pgtype.Timestamptz{Time: user.JoinedAt, Valid: true},
		CreatedAt:        pgtype.Timestamptz{Time: user.CreatedAt, Valid: true},
		UpdatedAt:        pgtype.Timestamptz{Time: user.UpdatedAt, Valid: true},
	})
	return err
}

func (r *UserRepo) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	row, err := r.q.GetUserByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return userFromRow(row), nil
}

func (r *UserRepo) GetUserByKratosID(ctx context.Context, kratosID string) (*domain.User, error) {
	row, err := r.q.GetUserByKratosID(ctx, kratosID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return userFromRow(row), nil
}

func (r *UserRepo) UpdateUserProfile(ctx context.Context, id, displayName, bio, avatarURL string) (*domain.User, error) {
	row, err := r.q.UpdateUserProfile(ctx, UpdateUserProfileParams{
		ID:          id,
		DisplayName: displayName,
		Bio:         bio,
		AvatarUrl:   avatarURL,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return userFromRow(row), nil
}

func (r *UserRepo) ListPendingUsers(ctx context.Context) ([]*domain.User, error) {
	rows, err := r.q.ListPendingUsers(ctx)
	if err != nil {
		return nil, err
	}
	users := make([]*domain.User, len(rows))
	for i, row := range rows {
		users[i] = userFromRow(row)
	}
	return users, nil
}

// ListDirectoryUsers returns one page of the member directory, filtered by a
// case-insensitive substring of the display name. An empty query lists
// everyone.
func (r *UserRepo) ListDirectoryUsers(ctx context.Context, query string, limit, offset int) ([]*domain.User, error) {
	rows, err := r.q.ListDirectoryUsers(ctx, ListDirectoryUsersParams{
		Query:     escapeLikePattern(query),
		RowLimit:  int32Bound(limit),
		RowOffset: int32Bound(offset),
	})
	if err != nil {
		return nil, err
	}
	users := make([]*domain.User, len(rows))
	for i, row := range rows {
		users[i] = userFromRow(row)
	}
	return users, nil
}

// CountDirectoryUsers counts everyone ListDirectoryUsers would page through for
// the same query.
func (r *UserRepo) CountDirectoryUsers(ctx context.Context, query string) (int64, error) {
	return r.q.CountDirectoryUsers(ctx, escapeLikePattern(query))
}

// escapeLikePattern neutralises the three characters LIKE reads as syntax, so a
// search term is matched literally.
//
// Without it "%" matches every resident and "_" matches any single character —
// a search that quietly returns the wrong people rather than failing. The
// escape character is backslash, which is what Postgres LIKE uses by default,
// so it must be escaped first or it would escape the escapes.
//
// Escaping happens here rather than in the service because it is a property of
// the SQL operator, not of the caller's intent: a service passing a plain
// substring is right, and a second storage backend would need its own rule.
func escapeLikePattern(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(s)
}

func (r *UserRepo) CountActiveMembers(ctx context.Context) (int64, error) {
	return r.q.CountUsersByMinRole(ctx)
}

func (r *UserRepo) UpdateUserRole(ctx context.Context, id string, role domain.Role) error {
	return r.q.UpdateUserRole(ctx, UpdateUserRoleParams{
		ID:   id,
		Role: string(role),
	})
}

func (r *UserRepo) DeactivateUser(ctx context.Context, id string) error {
	return r.q.DeactivateUser(ctx, id)
}

func (r *UserRepo) UpdateUserTrustScore(ctx context.Context, id string, score float64) error {
	return r.q.UpdateUserTrustScore(ctx, UpdateUserTrustScoreParams{
		ID:         id,
		TrustScore: score,
	})
}

// SetUserMutedUntil records when a mute expires. A nil until lifts the mute.
func (r *UserRepo) SetUserMutedUntil(ctx context.Context, id string, until *time.Time) error {
	muted := pgtype.Timestamptz{}
	if until != nil {
		muted = pgtype.Timestamptz{Time: *until, Valid: true}
	}
	return r.q.SetUserMutedUntil(ctx, SetUserMutedUntilParams{
		ID:         id,
		MutedUntil: muted,
	})
}

// SetUserSuspendedUntil records when a suspension expires. A nil until lifts it.
func (r *UserRepo) SetUserSuspendedUntil(ctx context.Context, id string, until *time.Time) error {
	suspended := pgtype.Timestamptz{}
	if until != nil {
		suspended = pgtype.Timestamptz{Time: *until, Valid: true}
	}
	return r.q.SetUserSuspendedUntil(ctx, SetUserSuspendedUntilParams{
		ID:             id,
		SuspendedUntil: suspended,
	})
}

// userFromRow builds the domain user, folding a suspension that is still in
// force into IsActive.
//
// IsActive is therefore not a plain mirror of users.is_active: it answers "may
// this account act right now", which is the question every caller of it was
// already asking. Deriving it here is what makes a suspension expire on its own
// everywhere at once — CanVouch, CanModerate and the RequireActive middleware
// all read this one field and none of them holds a clock. The alternative was a
// clock parameter on each of them, and a suspension that went on applying
// wherever one was forgotten.
//
// The column keeps its own meaning and is never written back from this value:
// the only query that writes is_active is DeactivateUser, and nothing
// round-trips a hydrated user into an update.
func userFromRow(row User) *domain.User {
	u := &domain.User{
		ID:               row.ID,
		KratosIdentityID: row.KratosIdentityID,
		DisplayName:      row.DisplayName,
		Bio:              row.Bio,
		AvatarURL:        row.AvatarUrl,
		TrustScore:       row.TrustScore,
		Role:             domain.Role(row.Role),
		JoinedAt:         row.JoinedAt.Time,
		CreatedAt:        row.CreatedAt.Time,
		UpdatedAt:        row.UpdatedAt.Time,
	}
	if row.TrustBelowSince.Valid {
		t := row.TrustBelowSince.Time
		u.TrustBelowSince = &t
	}
	if row.MutedUntil.Valid {
		t := row.MutedUntil.Time
		u.MutedUntil = &t
	}
	if row.SuspendedUntil.Valid {
		t := row.SuspendedUntil.Time
		u.SuspendedUntil = &t
	}
	// time.Now() rather than an injected clock: the queries that filter on this
	// same condition compare against the database's NOW(), and a second clock
	// that could be set independently would let the two disagree about who is
	// suspended.
	u.IsActive = row.IsActive && !u.IsSuspended(time.Now())
	return u
}

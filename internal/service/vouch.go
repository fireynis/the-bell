package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/google/uuid"
)

const dailyVouchLimit = 3

// VouchRepository abstracts vouch persistence using domain types.
type VouchRepository interface {
	CreateVouch(ctx context.Context, vouch *domain.Vouch) error
	GetVouchByID(ctx context.Context, id string) (*domain.Vouch, error)
	GetVouchByPair(ctx context.Context, voucherID, voucheeID string) (*domain.Vouch, error)
	CountVouchesByVoucherSince(ctx context.Context, voucherID string, since time.Time) (int64, error)
	ListActiveVouchesByVouchee(ctx context.Context, voucheeID string) ([]*domain.Vouch, error)
	ListActiveVouchesByVoucher(ctx context.Context, voucherID string) ([]*domain.Vouch, error)
	RevokeVouch(ctx context.Context, id string) error
}

// GraphQuerier abstracts trust-graph edge operations.
type GraphQuerier interface {
	AddVouchEdge(ctx context.Context, voucherID, voucheeID string) error
	RemoveVouchEdge(ctx context.Context, voucherID, voucheeID string) error
	HasCyclicVouch(ctx context.Context, voucherID, voucheeID string) (bool, error)
}

// UserGetter retrieves users and updates their roles.
type UserGetter interface {
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
	UpdateUserRole(ctx context.Context, id string, role domain.Role) error
}

// VouchService orchestrates vouch business logic.
type VouchService struct {
	vouches VouchRepository
	graph   GraphQuerier
	users   UserGetter
	now     func() time.Time
}

func NewVouchService(vouches VouchRepository, graph GraphQuerier, users UserGetter, clock func() time.Time) *VouchService {
	if clock == nil {
		clock = time.Now
	}
	return &VouchService{
		vouches: vouches,
		graph:   graph,
		users:   users,
		now:     clock,
	}
}

// Vouch creates a new vouch from voucherID to voucheeID.
// It enforces: no self-vouch, trust >= 60, no duplicate pair, daily limit of 3,
// and no graph cycles. On success it also promotes a pending vouchee to member.
func (s *VouchService) Vouch(ctx context.Context, voucherID, voucheeID string) (*domain.Vouch, error) {
	if voucherID == voucheeID {
		return nil, fmt.Errorf("%w: cannot vouch for yourself", ErrValidation)
	}

	voucher, err := s.users.GetUserByID(ctx, voucherID)
	if err != nil {
		return nil, fmt.Errorf("looking up voucher: %w", err)
	}

	if !voucher.CanVouch() {
		return nil, fmt.Errorf("%w: voucher does not meet trust requirements", ErrForbidden)
	}

	if _, err := s.users.GetUserByID(ctx, voucheeID); err != nil {
		return nil, fmt.Errorf("looking up vouchee: %w", err)
	}

	existing, err := s.vouches.GetVouchByPair(ctx, voucherID, voucheeID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("checking existing vouch: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("%w: vouch already exists for this pair", ErrValidation)
	}

	now := s.now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	count, err := s.vouches.CountVouchesByVoucherSince(ctx, voucherID, dayStart)
	if err != nil {
		return nil, fmt.Errorf("counting daily vouches: %w", err)
	}
	if count >= dailyVouchLimit {
		return nil, fmt.Errorf("%w: daily vouch limit (%d) reached", ErrValidation, dailyVouchLimit)
	}

	hasCycle, err := s.graph.HasCyclicVouch(ctx, voucherID, voucheeID)
	if err != nil {
		return nil, fmt.Errorf("checking cycle: %w", err)
	}
	if hasCycle {
		return nil, fmt.Errorf("%w: vouch would create a cycle in the trust graph", ErrValidation)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generating vouch id: %w", err)
	}

	vouch := &domain.Vouch{
		ID:        id.String(),
		VoucherID: voucherID,
		VoucheeID: voucheeID,
		Status:    domain.VouchActive,
		CreatedAt: now,
	}

	if err := s.vouches.CreateVouch(ctx, vouch); err != nil {
		return nil, fmt.Errorf("creating vouch: %w", err)
	}

	if err := s.graph.AddVouchEdge(ctx, voucherID, voucheeID); err != nil {
		return nil, fmt.Errorf("adding graph edge: %w", err)
	}

	// Promote pending users to member on first vouch received.
	vouchee, err := s.users.GetUserByID(ctx, voucheeID)
	if err != nil {
		return vouch, nil // vouch succeeded; promotion is best-effort
	}
	if vouchee.Role == domain.RolePending {
		_ = s.users.UpdateUserRole(ctx, voucheeID, domain.RoleMember)
	}

	return vouch, nil
}

// ListReceivedVouches returns active vouches received by the given user.
func (s *VouchService) ListReceivedVouches(ctx context.Context, userID string) ([]*domain.Vouch, error) {
	return s.vouches.ListActiveVouchesByVouchee(ctx, userID)
}

// ListGivenVouches returns active vouches given by the given user.
func (s *VouchService) ListGivenVouches(ctx context.Context, userID string) ([]*domain.Vouch, error) {
	return s.vouches.ListActiveVouchesByVoucher(ctx, userID)
}

// Revoke revokes an existing vouch. Only the original voucher, a moderator,
// or a council member can revoke.
func (s *VouchService) Revoke(ctx context.Context, vouchID, actorID string) error {
	vouch, err := s.vouches.GetVouchByID(ctx, vouchID)
	if err != nil {
		return fmt.Errorf("looking up vouch: %w", err)
	}

	if vouch.Status == domain.VouchRevoked {
		return fmt.Errorf("%w: vouch is already revoked", ErrValidation)
	}

	actor, err := s.users.GetUserByID(ctx, actorID)
	if err != nil {
		return fmt.Errorf("looking up actor: %w", err)
	}

	if vouch.VoucherID != actorID && !actor.CanModerate() {
		return ErrForbidden
	}

	if err := s.vouches.RevokeVouch(ctx, vouchID); err != nil {
		return fmt.Errorf("revoking vouch: %w", err)
	}

	if err := s.graph.RemoveVouchEdge(ctx, vouch.VoucherID, vouch.VoucheeID); err != nil {
		return fmt.Errorf("removing graph edge: %w", err)
	}

	return nil
}

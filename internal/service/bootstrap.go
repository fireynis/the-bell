package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/google/uuid"
)

// KratosAdmin creates identities via the Kratos Admin API.
type KratosAdmin interface {
	CreateIdentity(ctx context.Context, email, displayName, password string) (kratosID string, usedPassword string, err error)
}

// ConfigRepository reads and writes town_config key-value pairs.
type ConfigRepository interface {
	SetTownConfig(ctx context.Context, key, value string) error
	GetTownConfig(ctx context.Context, key string) (string, error)
	ListTownConfig(ctx context.Context) (map[string]string, error)
}

// Transactor wraps a function in a database transaction, providing
// transaction-scoped repository instances.
type Transactor interface {
	InTx(ctx context.Context, fn func(users UserRepository, config ConfigRepository) error) error
}

// BootstrapService handles initial town setup.
type BootstrapService struct {
	kratos KratosAdmin
	config ConfigRepository
	tx     Transactor
	now    func() time.Time
}

func NewBootstrapService(kratos KratosAdmin, config ConfigRepository, tx Transactor, clock func() time.Time) *BootstrapService {
	if clock == nil {
		clock = time.Now
	}
	return &BootstrapService{
		kratos: kratos,
		config: config,
		tx:     tx,
		now:    clock,
	}
}

// SetupResult holds the outcome of a bootstrap setup.
type SetupResult struct {
	Members []SetupMember
}

// SetupMember represents a council member created during setup.
type SetupMember struct {
	Email    string
	Password string
}

// Setup creates Kratos identities for the given emails, provisions local users
// as council members with max trust, optionally sets the town name, and enables
// bootstrap mode.
func (s *BootstrapService) Setup(ctx context.Context, emails []string, townName string) (*SetupResult, error) {
	if len(emails) == 0 {
		return nil, fmt.Errorf("%w: at least one council email is required", ErrValidation)
	}

	// Idempotency guard: if already bootstrapped, return early.
	val, err := s.config.GetTownConfig(ctx, "bootstrap_mode")
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("checking bootstrap status: %w", err)
	}
	if val == "true" {
		return nil, fmt.Errorf("%w: town is already bootstrapped", ErrValidation)
	}

	// Phase 1: Create Kratos identities (external, non-transactional).
	type identity struct {
		email    string
		kratosID string
		password string
	}
	identities := make([]identity, 0, len(emails))
	for _, email := range emails {
		kratosID, password, err := s.kratos.CreateIdentity(ctx, email, email, "")
		if err != nil {
			return nil, fmt.Errorf("creating kratos identity for %s: %w", email, err)
		}
		identities = append(identities, identity{email: email, kratosID: kratosID, password: password})
	}

	// Phase 2: Create local users + set config atomically in a transaction.
	err = s.tx.InTx(ctx, func(users UserRepository, config ConfigRepository) error {
		for _, ident := range identities {
			id, err := uuid.NewV7()
			if err != nil {
				return fmt.Errorf("generating user id: %w", err)
			}

			now := s.now()
			user := &domain.User{
				ID:               id.String(),
				KratosIdentityID: ident.kratosID,
				DisplayName:      ident.email,
				TrustScore:       100.0,
				Role:             domain.RoleCouncil,
				IsActive:         true,
				JoinedAt:         now,
				CreatedAt:        now,
				UpdatedAt:        now,
			}

			if err := users.CreateUser(ctx, user); err != nil {
				return fmt.Errorf("creating local user for %s: %w", ident.email, err)
			}
		}

		if err := config.SetTownConfig(ctx, "bootstrap_mode", "true"); err != nil {
			return fmt.Errorf("setting bootstrap mode: %w", err)
		}

		if townName != "" {
			if err := config.SetTownConfig(ctx, "town_name", townName); err != nil {
				return fmt.Errorf("setting town name: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	result := &SetupResult{
		Members: make([]SetupMember, len(identities)),
	}
	for i, ident := range identities {
		result.Members[i] = SetupMember{
			Email:    ident.email,
			Password: ident.password,
		}
	}
	return result, nil
}

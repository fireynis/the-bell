package service

import (
	"context"
	"fmt"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/google/uuid"
)

// KratosAdmin creates identities via the Kratos Admin API.
type KratosAdmin interface {
	CreateIdentity(ctx context.Context, email, displayName, password string) (kratosID string, err error)
}

// ConfigRepository reads and writes town_config key-value pairs.
type ConfigRepository interface {
	SetTownConfig(ctx context.Context, key, value string) error
	GetTownConfig(ctx context.Context, key string) (string, error)
}

// BootstrapService handles initial town setup.
type BootstrapService struct {
	users  UserRepository
	kratos KratosAdmin
	config ConfigRepository
	now    func() time.Time
}

func NewBootstrapService(users UserRepository, kratos KratosAdmin, config ConfigRepository, clock func() time.Time) *BootstrapService {
	if clock == nil {
		clock = time.Now
	}
	return &BootstrapService{
		users:  users,
		kratos: kratos,
		config: config,
		now:    clock,
	}
}

// Setup creates Kratos identities for the given emails, provisions local users
// as council members with max trust, and enables bootstrap mode.
func (s *BootstrapService) Setup(ctx context.Context, emails []string) error {
	if len(emails) == 0 {
		return fmt.Errorf("%w: at least one council email is required", ErrValidation)
	}

	for _, email := range emails {
		kratosID, err := s.kratos.CreateIdentity(ctx, email, email, "")
		if err != nil {
			return fmt.Errorf("creating kratos identity for %s: %w", email, err)
		}

		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("generating user id: %w", err)
		}

		now := s.now()
		user := &domain.User{
			ID:               id.String(),
			KratosIdentityID: kratosID,
			DisplayName:      email,
			TrustScore:       100.0,
			Role:             domain.RoleCouncil,
			IsActive:         true,
			JoinedAt:         now,
			CreatedAt:        now,
			UpdatedAt:        now,
		}

		if err := s.users.CreateUser(ctx, user); err != nil {
			return fmt.Errorf("creating local user for %s: %w", email, err)
		}
	}

	if err := s.config.SetTownConfig(ctx, "bootstrap_mode", "true"); err != nil {
		return fmt.Errorf("setting bootstrap mode: %w", err)
	}

	return nil
}

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// mockKratosAdmin implements KratosAdmin for testing.
type mockKratosAdmin struct {
	identities map[string]string // email → kratosID
	createErr  error
}

func newMockKratosAdmin() *mockKratosAdmin {
	return &mockKratosAdmin{identities: make(map[string]string)}
}

func (m *mockKratosAdmin) CreateIdentity(_ context.Context, email, _, _ string) (string, error) {
	if m.createErr != nil {
		return "", m.createErr
	}
	id := "kratos-" + email
	m.identities[email] = id
	return id, nil
}

// mockConfigRepo implements ConfigRepository for testing.
type mockConfigRepo struct {
	config map[string]string
	setErr error
}

func newMockConfigRepo() *mockConfigRepo {
	return &mockConfigRepo{config: make(map[string]string)}
}

func (m *mockConfigRepo) SetTownConfig(_ context.Context, key, value string) error {
	if m.setErr != nil {
		return m.setErr
	}
	m.config[key] = value
	return nil
}

func (m *mockConfigRepo) GetTownConfig(_ context.Context, key string) (string, error) {
	v, ok := m.config[key]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func TestBootstrapService_Setup_HappyPath(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	userRepo := newMockUserRepo()
	kratosAdmin := newMockKratosAdmin()
	configRepo := newMockConfigRepo()
	svc := NewBootstrapService(userRepo, kratosAdmin, configRepo, func() time.Time { return now })

	emails := []string{"alice@town.example", "bob@town.example"}
	err := svc.Setup(context.Background(), emails)
	if err != nil {
		t.Fatalf("Setup() unexpected error: %v", err)
	}

	if len(userRepo.users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(userRepo.users))
	}

	for _, email := range emails {
		kratosID := "kratos-" + email
		user, ok := userRepo.byKratos[kratosID]
		if !ok {
			t.Fatalf("user with kratos ID %q not found", kratosID)
		}
		if user.Role != domain.RoleCouncil {
			t.Errorf("user %s role = %q, want %q", email, user.Role, domain.RoleCouncil)
		}
		if user.TrustScore != 100.0 {
			t.Errorf("user %s trust = %f, want 100.0", email, user.TrustScore)
		}
		if user.DisplayName != email {
			t.Errorf("user %s display name = %q, want %q", email, user.DisplayName, email)
		}
	}

	if configRepo.config["bootstrap_mode"] != "true" {
		t.Errorf("bootstrap_mode = %q, want %q", configRepo.config["bootstrap_mode"], "true")
	}
}

func TestBootstrapService_Setup_EmptyEmails(t *testing.T) {
	svc := NewBootstrapService(newMockUserRepo(), newMockKratosAdmin(), newMockConfigRepo(), nil)

	err := svc.Setup(context.Background(), nil)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Setup(nil) error = %v, want %v", err, ErrValidation)
	}

	err = svc.Setup(context.Background(), []string{})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Setup([]) error = %v, want %v", err, ErrValidation)
	}
}

func TestBootstrapService_Setup_KratosError(t *testing.T) {
	kratosAdmin := newMockKratosAdmin()
	kratosAdmin.createErr = errors.New("kratos unavailable")
	svc := NewBootstrapService(newMockUserRepo(), kratosAdmin, newMockConfigRepo(), nil)

	err := svc.Setup(context.Background(), []string{"alice@town.example"})
	if err == nil {
		t.Fatal("Setup() expected error, got nil")
	}
}

func TestBootstrapService_Setup_UserCreateError(t *testing.T) {
	userRepo := newMockUserRepo()
	userRepo.createErr = errors.New("db connection failed")
	svc := NewBootstrapService(userRepo, newMockKratosAdmin(), newMockConfigRepo(), nil)

	err := svc.Setup(context.Background(), []string{"alice@town.example"})
	if err == nil {
		t.Fatal("Setup() expected error, got nil")
	}
}

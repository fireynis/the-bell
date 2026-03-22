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

func (m *mockKratosAdmin) CreateIdentity(_ context.Context, email, _, _ string) (string, string, error) {
	if m.createErr != nil {
		return "", "", m.createErr
	}
	id := "kratos-" + email
	m.identities[email] = id
	return id, "generated-password-" + email, nil
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

func (m *mockConfigRepo) ListTownConfig(_ context.Context) (map[string]string, error) {
	result := make(map[string]string, len(m.config))
	for k, v := range m.config {
		result[k] = v
	}
	return result, nil
}

// mockTransactor implements Transactor by passing through to the provided repos.
type mockTransactor struct {
	users  UserRepository
	config ConfigRepository
	txErr  error
}

func (m *mockTransactor) InTx(_ context.Context, fn func(UserRepository, ConfigRepository) error) error {
	if m.txErr != nil {
		return m.txErr
	}
	return fn(m.users, m.config)
}

func newBootstrapTestHarness(clock func() time.Time) (*mockUserRepo, *mockKratosAdmin, *mockConfigRepo, *mockTransactor, *BootstrapService) {
	userRepo := newMockUserRepo()
	kratosAdmin := newMockKratosAdmin()
	configRepo := newMockConfigRepo()
	tx := &mockTransactor{users: userRepo, config: configRepo}
	svc := NewBootstrapService(kratosAdmin, configRepo, tx, clock)
	return userRepo, kratosAdmin, configRepo, tx, svc
}

func TestBootstrapService_Setup_HappyPath(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	userRepo, _, configRepo, _, svc := newBootstrapTestHarness(func() time.Time { return now })

	emails := []string{"alice@town.example", "bob@town.example"}
	result, err := svc.Setup(context.Background(), emails, "Springfield")
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
	if configRepo.config["town_name"] != "Springfield" {
		t.Errorf("town_name = %q, want %q", configRepo.config["town_name"], "Springfield")
	}

	if len(result.Members) != 2 {
		t.Fatalf("expected 2 members in result, got %d", len(result.Members))
	}
	for _, m := range result.Members {
		if m.Email == "" {
			t.Error("expected member email, got empty string")
		}
		if m.Password == "" {
			t.Error("expected member password, got empty string")
		}
	}
}

func TestBootstrapService_Setup_EmptyEmails(t *testing.T) {
	_, _, _, _, svc := newBootstrapTestHarness(nil)

	_, err := svc.Setup(context.Background(), nil, "")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Setup(nil) error = %v, want %v", err, ErrValidation)
	}

	_, err = svc.Setup(context.Background(), []string{}, "")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Setup([]) error = %v, want %v", err, ErrValidation)
	}
}

func TestBootstrapService_Setup_KratosError(t *testing.T) {
	_, kratosAdmin, _, _, svc := newBootstrapTestHarness(nil)
	kratosAdmin.createErr = errors.New("kratos unavailable")

	_, err := svc.Setup(context.Background(), []string{"alice@town.example"}, "")
	if err == nil {
		t.Fatal("Setup() expected error, got nil")
	}
}

func TestBootstrapService_Setup_UserCreateError(t *testing.T) {
	userRepo, _, _, _, svc := newBootstrapTestHarness(nil)
	userRepo.createErr = errors.New("db connection failed")

	_, err := svc.Setup(context.Background(), []string{"alice@town.example"}, "")
	if err == nil {
		t.Fatal("Setup() expected error, got nil")
	}
}

func TestBootstrapService_Setup_AlreadyBootstrapped(t *testing.T) {
	userRepo, kratosAdmin, configRepo, _, svc := newBootstrapTestHarness(nil)
	configRepo.config["bootstrap_mode"] = "true"

	_, err := svc.Setup(context.Background(), []string{"alice@town.example"}, "")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Setup() error = %v, want ErrValidation", err)
	}
	if len(userRepo.users) != 0 {
		t.Fatal("expected no users created when already bootstrapped")
	}
	if len(kratosAdmin.identities) != 0 {
		t.Fatal("expected no Kratos identities created when already bootstrapped")
	}
}

func TestBootstrapService_Setup_TxError(t *testing.T) {
	_, _, _, tx, svc := newBootstrapTestHarness(nil)
	tx.txErr = errors.New("tx begin failed")

	_, err := svc.Setup(context.Background(), []string{"alice@town.example"}, "")
	if err == nil {
		t.Fatal("Setup() expected error, got nil")
	}
}

func TestBootstrapService_Setup_NoTownName(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	_, _, configRepo, _, svc := newBootstrapTestHarness(func() time.Time { return now })

	_, err := svc.Setup(context.Background(), []string{"alice@town.example"}, "")
	if err != nil {
		t.Fatalf("Setup() unexpected error: %v", err)
	}

	if _, ok := configRepo.config["town_name"]; ok {
		t.Error("expected town_name not to be set when empty")
	}
}

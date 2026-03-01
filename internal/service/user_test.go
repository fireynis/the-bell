package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// mockUserRepo is an in-memory UserRepository for testing.
type mockUserRepo struct {
	users    map[string]*domain.User // keyed by ID
	byKratos map[string]*domain.User // keyed by KratosIdentityID

	getByKratosErr error // if set, GetUserByKratosID returns this error
	createErr      error // if set, CreateUser returns this error
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{
		users:    make(map[string]*domain.User),
		byKratos: make(map[string]*domain.User),
	}
}

func (m *mockUserRepo) CreateUser(_ context.Context, user *domain.User) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.users[user.ID] = user
	m.byKratos[user.KratosIdentityID] = user
	return nil
}

func (m *mockUserRepo) GetUserByID(_ context.Context, id string) (*domain.User, error) {
	u, ok := m.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	return u, nil
}

func (m *mockUserRepo) GetUserByKratosID(_ context.Context, kratosID string) (*domain.User, error) {
	if m.getByKratosErr != nil {
		return nil, m.getByKratosErr
	}
	u, ok := m.byKratos[kratosID]
	if !ok {
		return nil, ErrNotFound
	}
	return u, nil
}

func (m *mockUserRepo) ListPendingUsers(_ context.Context) ([]*domain.User, error) {
	var pending []*domain.User
	for _, u := range m.users {
		if u.Role == domain.RolePending && u.IsActive {
			pending = append(pending, u)
		}
	}
	return pending, nil
}

func (m *mockUserRepo) CountActiveMembers(_ context.Context) (int64, error) {
	var count int64
	for _, u := range m.users {
		if u.IsActive && (u.Role == domain.RoleMember || u.Role == domain.RoleModerator || u.Role == domain.RoleCouncil) {
			count++
		}
	}
	return count, nil
}

func (m *mockUserRepo) UpdateUserRole(_ context.Context, id string, role domain.Role) error {
	u, ok := m.users[id]
	if !ok {
		return ErrNotFound
	}
	u.Role = role
	return nil
}

func (m *mockUserRepo) UpdateUserProfile(_ context.Context, id, displayName, bio, avatarURL string) (*domain.User, error) {
	u, ok := m.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	u.DisplayName = displayName
	u.Bio = bio
	u.AvatarURL = avatarURL
	return u, nil
}

func TestUserService_FindOrCreate_NewUser(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockUserRepo()
	svc := NewUserService(repo, func() time.Time { return now })

	user, err := svc.FindOrCreate(context.Background(), "kratos-abc-123")
	if err != nil {
		t.Fatalf("FindOrCreate() unexpected error: %v", err)
	}

	if user.ID == "" {
		t.Error("FindOrCreate() returned empty ID")
	}
	if user.KratosIdentityID != "kratos-abc-123" {
		t.Errorf("KratosIdentityID = %q, want %q", user.KratosIdentityID, "kratos-abc-123")
	}
	if user.TrustScore != 50.0 {
		t.Errorf("TrustScore = %f, want 50.0", user.TrustScore)
	}
	if user.Role != domain.RolePending {
		t.Errorf("Role = %q, want %q", user.Role, domain.RolePending)
	}
	if !user.IsActive {
		t.Error("IsActive = false, want true")
	}
	if !user.JoinedAt.Equal(now) {
		t.Errorf("JoinedAt = %v, want %v", user.JoinedAt, now)
	}
	if !user.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", user.CreatedAt, now)
	}

	// Verify user was stored in repo
	if _, ok := repo.users[user.ID]; !ok {
		t.Error("user not stored in repository")
	}
}

func TestUserService_FindOrCreate_ExistingUser(t *testing.T) {
	repo := newMockUserRepo()
	svc := NewUserService(repo, nil)

	existing := &domain.User{
		ID:               "user-existing",
		KratosIdentityID: "kratos-abc-123",
		DisplayName:      "Existing User",
		TrustScore:       75.0,
		Role:             domain.RoleMember,
		IsActive:         true,
	}
	repo.users[existing.ID] = existing
	repo.byKratos[existing.KratosIdentityID] = existing

	user, err := svc.FindOrCreate(context.Background(), "kratos-abc-123")
	if err != nil {
		t.Fatalf("FindOrCreate() unexpected error: %v", err)
	}

	if user.ID != "user-existing" {
		t.Errorf("ID = %q, want %q (should return existing user)", user.ID, "user-existing")
	}
	if user.TrustScore != 75.0 {
		t.Errorf("TrustScore = %f, want 75.0 (should not reset)", user.TrustScore)
	}
	if user.Role != domain.RoleMember {
		t.Errorf("Role = %q, want %q (should not reset)", user.Role, domain.RoleMember)
	}

	// Should not have created a second user
	if len(repo.users) != 1 {
		t.Errorf("repo has %d users, want 1", len(repo.users))
	}
}

func TestUserService_FindOrCreate_LookupError(t *testing.T) {
	repo := newMockUserRepo()
	repo.getByKratosErr = errors.New("connection refused")
	svc := NewUserService(repo, nil)

	_, err := svc.FindOrCreate(context.Background(), "kratos-abc-123")
	if err == nil {
		t.Fatal("FindOrCreate() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "looking up user by kratos id") {
		t.Errorf("error = %q, want wrapped lookup error", err)
	}
}

func TestUserService_FindOrCreate_CreateError(t *testing.T) {
	repo := newMockUserRepo()
	repo.createErr = errors.New("unique constraint violation")
	svc := NewUserService(repo, nil)

	_, err := svc.FindOrCreate(context.Background(), "kratos-new")
	if err == nil {
		t.Fatal("FindOrCreate() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "creating user") {
		t.Errorf("error = %q, want wrapped create error", err)
	}
}

func TestUserService_FindByKratosID(t *testing.T) {
	repo := newMockUserRepo()
	svc := NewUserService(repo, nil)

	// FindByKratosID delegates to FindOrCreate, so calling it for a new
	// kratos ID should auto-provision a user.
	user, err := svc.FindByKratosID(context.Background(), "kratos-new")
	if err != nil {
		t.Fatalf("FindByKratosID() unexpected error: %v", err)
	}
	if user == nil {
		t.Fatal("FindByKratosID() returned nil user")
	}
	if user.KratosIdentityID != "kratos-new" {
		t.Errorf("KratosIdentityID = %q, want %q", user.KratosIdentityID, "kratos-new")
	}
}

func TestUserService_GetByID(t *testing.T) {
	repo := newMockUserRepo()
	svc := NewUserService(repo, nil)

	existing := &domain.User{
		ID:               "user-1",
		KratosIdentityID: "kratos-1",
		DisplayName:      "Test User",
		Role:             domain.RoleMember,
	}
	repo.users["user-1"] = existing

	tests := []struct {
		name    string
		id      string
		wantErr error
	}{
		{"existing user", "user-1", nil},
		{"not found", "user-999", ErrNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := svc.GetByID(context.Background(), tt.id)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("GetByID() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetByID() unexpected error: %v", err)
			}
			if user.ID != tt.id {
				t.Errorf("ID = %q, want %q", user.ID, tt.id)
			}
		})
	}
}

func TestUserService_UpdateProfile(t *testing.T) {
	tests := []struct {
		name        string
		seed        *domain.User
		displayName string
		bio         string
		avatarURL   string
		wantErr     error
	}{
		{
			name: "valid update",
			seed: &domain.User{
				ID:          "user-1",
				DisplayName: "Old Name",
				Role:        domain.RoleMember,
			},
			displayName: "New Name",
			bio:         "A short bio",
			avatarURL:   "/avatars/pic.jpg",
		},
		{
			name: "empty bio is valid",
			seed: &domain.User{
				ID:          "user-2",
				DisplayName: "User",
				Role:        domain.RoleMember,
			},
			displayName: "User",
			bio:         "",
			avatarURL:   "",
		},
		{
			name: "empty display name",
			seed: &domain.User{
				ID:          "user-3",
				DisplayName: "User",
				Role:        domain.RoleMember,
			},
			displayName: "",
			bio:         "bio",
			avatarURL:   "",
			wantErr:     ErrValidation,
		},
		{
			name: "whitespace-only display name",
			seed: &domain.User{
				ID:          "user-4",
				DisplayName: "User",
				Role:        domain.RoleMember,
			},
			displayName: "   \t  ",
			bio:         "bio",
			avatarURL:   "",
			wantErr:     ErrValidation,
		},
		{
			name:        "not found",
			seed:        nil,
			displayName: "Name",
			bio:         "bio",
			avatarURL:   "",
			wantErr:     ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockUserRepo()
			svc := NewUserService(repo, nil)

			userID := "nonexistent"
			if tt.seed != nil {
				repo.users[tt.seed.ID] = tt.seed
				userID = tt.seed.ID
			}

			user, err := svc.UpdateProfile(context.Background(), userID, tt.displayName, tt.bio, tt.avatarURL)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("UpdateProfile() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("UpdateProfile() unexpected error: %v", err)
			}
			if user.DisplayName != tt.displayName {
				t.Errorf("DisplayName = %q, want %q", user.DisplayName, tt.displayName)
			}
			if user.Bio != tt.bio {
				t.Errorf("Bio = %q, want %q", user.Bio, tt.bio)
			}
			if user.AvatarURL != tt.avatarURL {
				t.Errorf("AvatarURL = %q, want %q", user.AvatarURL, tt.avatarURL)
			}
		})
	}
}

func TestUserService_UpdateProfile_DisplayNameTooLong(t *testing.T) {
	repo := newMockUserRepo()
	svc := NewUserService(repo, nil)

	seed := &domain.User{ID: "user-1", DisplayName: "User"}
	repo.users[seed.ID] = seed

	longName := make([]byte, maxDisplayNameLength+1)
	for i := range longName {
		longName[i] = 'a'
	}

	_, err := svc.UpdateProfile(context.Background(), "user-1", string(longName), "", "")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("UpdateProfile() error = %v, want %v", err, ErrValidation)
	}
}

func TestUserService_UpdateProfile_BioTooLong(t *testing.T) {
	repo := newMockUserRepo()
	svc := NewUserService(repo, nil)

	seed := &domain.User{ID: "user-1", DisplayName: "User"}
	repo.users[seed.ID] = seed

	longBio := make([]byte, maxBioLength+1)
	for i := range longBio {
		longBio[i] = 'a'
	}

	_, err := svc.UpdateProfile(context.Background(), "user-1", "Valid Name", string(longBio), "")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("UpdateProfile() error = %v, want %v", err, ErrValidation)
	}
}

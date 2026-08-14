package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

func TestUserService_FindOrCreate_NewUser(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockUserRepo()
	svc := NewUserService(repo, func() time.Time { return now })

	user, err := svc.FindOrCreate(context.Background(), "kratos-abc-123", "Ada Lovelace")
	if err != nil {
		t.Fatalf("FindOrCreate() unexpected error: %v", err)
	}

	if user.ID == "" {
		t.Error("FindOrCreate() returned empty ID")
	}
	if user.KratosIdentityID != "kratos-abc-123" {
		t.Errorf("KratosIdentityID = %q, want %q", user.KratosIdentityID, "kratos-abc-123")
	}
	if user.DisplayName != "Ada Lovelace" {
		t.Errorf("DisplayName = %q, want %q (the identity's name trait)", user.DisplayName, "Ada Lovelace")
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

	user, err := svc.FindOrCreate(context.Background(), "kratos-abc-123", "Trait Name")
	if err != nil {
		t.Fatalf("FindOrCreate() unexpected error: %v", err)
	}

	if user.ID != "user-existing" {
		t.Errorf("ID = %q, want %q (should return existing user)", user.ID, "user-existing")
	}
	// The trait passed above differs from the stored name. The stored one wins:
	// a user who renamed themselves in-app must not have that edit reverted by
	// their next request.
	if user.DisplayName != "Existing User" {
		t.Errorf("DisplayName = %q, want %q (trait must not clobber an edited name)", user.DisplayName, "Existing User")
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

// A missing or blank `name` trait must still produce an account. Sign-up is
// the one flow where failing closed means the person cannot join at all, and a
// blank display name is exactly the state every user was in before the trait
// was threaded through — recoverable through UpdateProfile.
func TestUserService_FindOrCreate_EmptyTraitStillCreatesTheUser(t *testing.T) {
	tests := []struct {
		name        string
		displayName string
	}{
		{"absent trait", ""},
		{"whitespace-only trait", "   \t\n "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockUserRepo()
			svc := NewUserService(repo, nil)

			user, err := svc.FindOrCreate(context.Background(), "kratos-nameless", tt.displayName)
			if err != nil {
				t.Fatalf("FindOrCreate() unexpected error: %v", err)
			}
			if user.DisplayName != "" {
				t.Errorf("DisplayName = %q, want empty", user.DisplayName)
			}
			if user.Role != domain.RolePending {
				t.Errorf("Role = %q, want %q", user.Role, domain.RolePending)
			}
			if _, ok := repo.users[user.ID]; !ok {
				t.Error("user not stored in repository")
			}
		})
	}
}

// The name is stored trimmed regardless of what the caller passes, so the
// invariant does not depend on the middleware having tidied it up first.
func TestUserService_FindOrCreate_TrimsTheTrait(t *testing.T) {
	repo := newMockUserRepo()
	svc := NewUserService(repo, nil)

	user, err := svc.FindOrCreate(context.Background(), "kratos-padded", "  Grace Hopper\n")
	if err != nil {
		t.Fatalf("FindOrCreate() unexpected error: %v", err)
	}
	if user.DisplayName != "Grace Hopper" {
		t.Errorf("DisplayName = %q, want %q", user.DisplayName, "Grace Hopper")
	}
}

// Regression guard for the join path this fix exists for: a user created
// through the middleware's entry point must reach the council's approval queue
// with a name on it, not just a Kratos ID.
func TestUserService_FindByKratosID_SeedsTheDisplayName(t *testing.T) {
	repo := newMockUserRepo()
	svc := NewUserService(repo, nil)

	user, err := svc.FindByKratosID(context.Background(), "kratos-new-member", "Ada Lovelace")
	if err != nil {
		t.Fatalf("FindByKratosID() unexpected error: %v", err)
	}
	if user.DisplayName != "Ada Lovelace" {
		t.Errorf("DisplayName = %q, want %q", user.DisplayName, "Ada Lovelace")
	}

	// Second request, same identity, a stale trait: the stored record wins.
	again, err := svc.FindByKratosID(context.Background(), "kratos-new-member", "Someone Else")
	if err != nil {
		t.Fatalf("FindByKratosID() unexpected error: %v", err)
	}
	if again.DisplayName != "Ada Lovelace" {
		t.Errorf("DisplayName = %q, want it unchanged at %q", again.DisplayName, "Ada Lovelace")
	}
	if len(repo.users) != 1 {
		t.Errorf("repo has %d users, want 1", len(repo.users))
	}
}

func TestUserService_FindOrCreate_LookupError(t *testing.T) {
	repo := newMockUserRepo()
	repo.getByKratosErr = errors.New("connection refused")
	svc := NewUserService(repo, nil)

	_, err := svc.FindOrCreate(context.Background(), "kratos-abc-123", "Ada")
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

	_, err := svc.FindOrCreate(context.Background(), "kratos-new", "Ada")
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
	user, err := svc.FindByKratosID(context.Background(), "kratos-new", "Ada")
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

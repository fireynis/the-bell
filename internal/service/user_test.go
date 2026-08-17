package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/fireynis/the-bell/internal/domain"
)

func TestUserService_FindOrCreate_NewUser(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockUserRepo()
	svc := NewUserService(repo, func() time.Time { return now })

	user, _, err := svc.FindOrCreate(context.Background(), "kratos-abc-123", "Ada Lovelace")
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

	user, _, err := svc.FindOrCreate(context.Background(), "kratos-abc-123", "Trait Name")
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

			user, _, err := svc.FindOrCreate(context.Background(), "kratos-nameless", tt.displayName)
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

	user, _, err := svc.FindOrCreate(context.Background(), "kratos-padded", "  Grace Hopper\n")
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

	user, _, err := svc.FindByKratosID(context.Background(), "kratos-new-member", "Ada Lovelace")
	if err != nil {
		t.Fatalf("FindByKratosID() unexpected error: %v", err)
	}
	if user.DisplayName != "Ada Lovelace" {
		t.Errorf("DisplayName = %q, want %q", user.DisplayName, "Ada Lovelace")
	}

	// Second request, same identity, a stale trait: the stored record wins.
	again, _, err := svc.FindByKratosID(context.Background(), "kratos-new-member", "Someone Else")
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

	_, _, err := svc.FindOrCreate(context.Background(), "kratos-abc-123", "Ada")
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

	_, _, err := svc.FindOrCreate(context.Background(), "kratos-new", "Ada")
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
	user, _, err := svc.FindByKratosID(context.Background(), "kratos-new", "Ada")
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

// --- display name backfill ---

// backfillStore is a user store that can list both rosters the backfill walks
// and can be made to fail one specific write. fakeUserStore covers neither, and
// the failure injection is what proves a single bad user does not end the run.
type backfillStore struct {
	users      []*domain.User
	byID       map[string]*domain.User
	pendingErr error
	activeErr  error
	updateErr  map[string]error
	updates    int
}

func newBackfillStore(users ...*domain.User) *backfillStore {
	s := &backfillStore{
		byID:      make(map[string]*domain.User, len(users)),
		updateErr: make(map[string]error),
	}
	for _, u := range users {
		s.users = append(s.users, u)
		s.byID[u.ID] = u
	}
	return s
}

func (s *backfillStore) CreateUser(_ context.Context, u *domain.User) error {
	s.users = append(s.users, u)
	s.byID[u.ID] = u
	return nil
}

func (s *backfillStore) GetUserByID(_ context.Context, id string) (*domain.User, error) {
	u, ok := s.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	return u, nil
}

func (s *backfillStore) GetUserByKratosID(_ context.Context, kratosID string) (*domain.User, error) {
	for _, u := range s.users {
		if u.KratosIdentityID == kratosID {
			return u, nil
		}
	}
	return nil, ErrNotFound
}

func (s *backfillStore) UpdateUserProfile(_ context.Context, id, displayName, bio, avatarURL string) (*domain.User, error) {
	if err := s.updateErr[id]; err != nil {
		return nil, err
	}
	u, ok := s.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	s.updates++
	u.DisplayName = displayName
	u.Bio = bio
	u.AvatarURL = avatarURL
	return u, nil
}

// The backfill never browses the directory and never touches a residency
// claim; these satisfy UserRepository so the store can still be handed to a
// UserService.
func (s *backfillStore) ListDirectoryUsers(_ context.Context, _ string, _, _ int) ([]*domain.User, error) {
	return nil, nil
}

func (s *backfillStore) SetUserResidencyClaim(_ context.Context, _, _ string) error {
	return nil
}

func (s *backfillStore) CountDirectoryUsers(_ context.Context, _ string) (int64, error) {
	return 0, nil
}

func (s *backfillStore) ListPendingUsers(_ context.Context) ([]*domain.User, error) {
	if s.pendingErr != nil {
		return nil, s.pendingErr
	}
	var out []*domain.User
	for _, u := range s.users {
		if u.Role == domain.RolePending {
			out = append(out, u)
		}
	}
	return out, nil
}

func (s *backfillStore) ListActiveNonBannedUsers(_ context.Context) ([]*domain.User, error) {
	if s.activeErr != nil {
		return nil, s.activeErr
	}
	var out []*domain.User
	for _, u := range s.users {
		if u.Role != domain.RolePending && u.Role != domain.RoleBanned {
			out = append(out, u)
		}
	}
	return out, nil
}

// fakeIdentities stands in for Kratos. A name absent from the map is an
// identity with no `name` trait, which the real client also reports as ("",
// nil) rather than an error.
type fakeIdentities struct {
	names   map[string]string
	errs    map[string]error
	lookups int
}

func (f *fakeIdentities) IdentityDisplayName(_ context.Context, kratosID string) (string, error) {
	f.lookups++
	if err := f.errs[kratosID]; err != nil {
		return "", err
	}
	return f.names[kratosID], nil
}

var (
	_ UserRepository    = (*backfillStore)(nil)
	_ PendingUserLister = (*backfillStore)(nil)
	_ ActiveUserLister  = (*backfillStore)(nil)
	_ IdentityDirectory = (*fakeIdentities)(nil)
)

func newBackfill(store *backfillStore, ids *fakeIdentities) *DisplayNameBackfill {
	return NewDisplayNameBackfill(store, store, ids, NewUserService(store, nil), nil)
}

// The case the command exists for: a user provisioned before the name trait was
// synced has an empty display name, and the trait is still sitting in Kratos.
func TestDisplayNameBackfill_FillsEmptyNames(t *testing.T) {
	store := newBackfillStore(
		&domain.User{ID: "u1", KratosIdentityID: "k1", Role: domain.RolePending, IsActive: true},
		&domain.User{ID: "u2", KratosIdentityID: "k2", Role: domain.RoleMember, IsActive: true, Bio: "keeps bees", AvatarURL: "/a/2.png"},
	)
	ids := &fakeIdentities{names: map[string]string{"k1": "Ada Lovelace", "k2": "  Grace Hopper \n"}}

	result, err := newBackfill(store, ids).Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if result.Updated != 2 || result.Errors != 0 || result.SkippedNamed != 0 || result.SkippedNoTrait != 0 {
		t.Fatalf("result = %+v, want 2 updated and nothing else", result)
	}
	if store.byID["u1"].DisplayName != "Ada Lovelace" {
		t.Errorf("u1 display name = %q, want %q", store.byID["u1"].DisplayName, "Ada Lovelace")
	}
	// The trait is stored trimmed, and the rest of the profile is untouched: the
	// backfill writes through UpdateProfile, which sets all three columns.
	if got := store.byID["u2"]; got.DisplayName != "Grace Hopper" || got.Bio != "keeps bees" || got.AvatarURL != "/a/2.png" {
		t.Errorf("u2 = %+v, want the trimmed name with bio and avatar intact", got)
	}
}

// Idempotence is the whole safety story for a command an operator may run
// twice: the second pass must find nothing to do and write nothing.
func TestDisplayNameBackfill_LeavesExistingNamesAlone(t *testing.T) {
	store := newBackfillStore(
		&domain.User{ID: "u1", KratosIdentityID: "k1", DisplayName: "Chosen Name", Role: domain.RoleMember, IsActive: true},
	)
	ids := &fakeIdentities{names: map[string]string{"k1": "Stale Trait"}}
	backfill := newBackfill(store, ids)

	result, err := backfill.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if result.SkippedNamed != 1 || result.Updated != 0 {
		t.Fatalf("result = %+v, want 1 skipped-already-named", result)
	}
	if store.byID["u1"].DisplayName != "Chosen Name" {
		t.Errorf("display name = %q, want the in-app name kept", store.byID["u1"].DisplayName)
	}
	// A name the user chose is theirs; the backfill must not even ask Kratos
	// what the trait says, let alone write it.
	if ids.lookups != 0 {
		t.Errorf("identity lookups = %d, want 0 for an already-named user", ids.lookups)
	}
	if store.updates != 0 {
		t.Errorf("writes = %d, want 0", store.updates)
	}

	// Second run: still nothing to do.
	again, err := backfill.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("second Run() unexpected error: %v", err)
	}
	if again.Updated != 0 || store.updates != 0 {
		t.Errorf("second run wrote %d names, want 0", again.Updated)
	}
}

// A blank stored name is blank whether it is "" or whitespace, and an identity
// whose trait is missing or blank leaves nothing to write.
func TestDisplayNameBackfill_SkipsWhenThereIsNoTrait(t *testing.T) {
	store := newBackfillStore(
		&domain.User{ID: "u1", KratosIdentityID: "k1", Role: domain.RolePending, IsActive: true},
		&domain.User{ID: "u2", KratosIdentityID: "k2", DisplayName: "   ", Role: domain.RoleMember, IsActive: true},
		&domain.User{ID: "u3", Role: domain.RoleMember, IsActive: true},
	)
	ids := &fakeIdentities{names: map[string]string{"k2": "  \t "}}

	result, err := newBackfill(store, ids).Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	// u1 has no trait at all, u2's trait is whitespace, u3 has no identity to
	// read one from. None is an error and none is a write.
	if result.SkippedNoTrait != 3 {
		t.Errorf("skipped-no-trait = %d, want 3 (result = %+v)", result.SkippedNoTrait, result)
	}
	if result.Updated != 0 || result.Errors != 0 || store.updates != 0 {
		t.Errorf("result = %+v with %d writes, want no updates and no errors", result, store.updates)
	}
	// A whitespace-only stored name counts as empty, not as already-named.
	if result.SkippedNamed != 0 {
		t.Errorf("skipped-already-named = %d, want 0", result.SkippedNamed)
	}
}

// Kratos allows a 255-character name; the app caps display names at 100 bytes
// and UpdateProfile rejects anything longer. Truncating is what keeps such a
// user from becoming an error the backfill can never clear.
func TestDisplayNameBackfill_TruncatesOverlongName(t *testing.T) {
	long := strings.Repeat("a", maxDisplayNameLength+50)
	store := newBackfillStore(&domain.User{ID: "u1", KratosIdentityID: "k1", Role: domain.RoleMember, IsActive: true})
	ids := &fakeIdentities{names: map[string]string{"k1": long}}

	result, err := newBackfill(store, ids).Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if result.Updated != 1 || result.Errors != 0 {
		t.Fatalf("result = %+v, want 1 updated and no errors", result)
	}

	got := store.byID["u1"].DisplayName
	if len(got) != maxDisplayNameLength {
		t.Errorf("stored length = %d, want %d", len(got), maxDisplayNameLength)
	}
	if got != long[:maxDisplayNameLength] {
		t.Errorf("stored = %q, want the first %d characters", got, maxDisplayNameLength)
	}
	if len(result.Changes) != 1 || !result.Changes[0].Truncated {
		t.Errorf("changes = %+v, want the change flagged as truncated so the run can report it", result.Changes)
	}
}

// --dry-run has to be genuinely read-only: it reports the same counts a real
// run would, and touches nothing.
func TestDisplayNameBackfill_DryRunWritesNothing(t *testing.T) {
	store := newBackfillStore(
		&domain.User{ID: "u1", KratosIdentityID: "k1", Role: domain.RolePending, IsActive: true},
		&domain.User{ID: "u2", KratosIdentityID: "k2", DisplayName: "Named", Role: domain.RoleMember, IsActive: true},
	)
	ids := &fakeIdentities{names: map[string]string{"k1": "Ada Lovelace"}}

	result, err := newBackfill(store, ids).Run(context.Background(), true)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if result.Updated != 1 || result.SkippedNamed != 1 {
		t.Errorf("result = %+v, want 1 would-update and 1 already-named", result)
	}
	if store.updates != 0 {
		t.Fatalf("dry run performed %d writes, want 0", store.updates)
	}
	if store.byID["u1"].DisplayName != "" {
		t.Errorf("u1 display name = %q, want it still empty after a dry run", store.byID["u1"].DisplayName)
	}
	if len(result.Changes) != 1 || result.Changes[0].DisplayName != "Ada Lovelace" {
		t.Errorf("changes = %+v, want the name the run would have written", result.Changes)
	}
}

// The run is a sweep, not a transaction. One identity Kratos will not return
// and one write the database rejects are counted and stepped over; every other
// user still gets their name.
func TestDisplayNameBackfill_OneFailureDoesNotStopTheRest(t *testing.T) {
	store := newBackfillStore(
		&domain.User{ID: "u1", KratosIdentityID: "k1", Role: domain.RolePending, IsActive: true},
		&domain.User{ID: "u2", KratosIdentityID: "k2", Role: domain.RoleMember, IsActive: true},
		&domain.User{ID: "u3", KratosIdentityID: "k3", Role: domain.RoleMember, IsActive: true},
	)
	store.updateErr["u2"] = errors.New("connection reset")
	ids := &fakeIdentities{
		names: map[string]string{"k1": "Ada", "k2": "Grace", "k3": "Katherine"},
		errs:  map[string]error{"k1": errors.New("404 no such identity")},
	}

	result, err := newBackfill(store, ids).Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run() returned a fatal error for a per-user failure: %v", err)
	}

	if result.Scanned != 3 {
		t.Errorf("scanned = %d, want 3", result.Scanned)
	}
	if result.Errors != 2 {
		t.Errorf("errors = %d, want 2 (one lookup, one write)", result.Errors)
	}
	if result.Updated != 1 {
		t.Errorf("updated = %d, want 1", result.Updated)
	}
	if store.byID["u3"].DisplayName != "Katherine" {
		t.Errorf("u3 display name = %q, want %q — the last user must still be reached", store.byID["u3"].DisplayName, "Katherine")
	}
	if store.byID["u2"].DisplayName != "" {
		t.Errorf("u2 display name = %q, want it unchanged after a failed write", store.byID["u2"].DisplayName)
	}
}

// A roster query that fails is fatal, unlike a per-user failure: continuing
// would report a tidy "0 errors" for a run that never saw most of the town.
func TestDisplayNameBackfill_ListingFailureIsFatal(t *testing.T) {
	tests := []struct {
		name string
		set  func(*backfillStore)
		want string
	}{
		{"pending roster", func(s *backfillStore) { s.pendingErr = errors.New("boom") }, "listing pending users"},
		{"active roster", func(s *backfillStore) { s.activeErr = errors.New("boom") }, "listing active users"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newBackfillStore(&domain.User{ID: "u1", KratosIdentityID: "k1", Role: domain.RoleMember})
			tt.set(store)

			_, err := newBackfill(store, &fakeIdentities{}).Run(context.Background(), false)
			if err == nil {
				t.Fatal("Run() succeeded despite a failed roster query")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to mention %q", err, tt.want)
			}
		})
	}
}

// The pending queue and the active roster are disjoint today, but a user
// appearing on both must be updated once, not twice.
func TestDisplayNameBackfill_DeduplicatesTheRosters(t *testing.T) {
	dup := &domain.User{ID: "u1", KratosIdentityID: "k1", Role: domain.RolePending, IsActive: true}
	store := newBackfillStore(dup)
	// Listed twice by the pending query and again by the active lister below,
	// as a widened query would do.
	store.users = append(store.users, dup)
	ids := &fakeIdentities{names: map[string]string{"k1": "Ada"}}

	backfill := NewDisplayNameBackfill(
		store,
		listerFunc(func(ctx context.Context) ([]*domain.User, error) { return []*domain.User{dup}, nil }),
		ids,
		NewUserService(store, nil),
		nil,
	)

	result, err := backfill.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if result.Scanned != 1 || result.Updated != 1 {
		t.Errorf("result = %+v, want the duplicate collapsed to one scanned and one updated", result)
	}
	if store.updates != 1 {
		t.Errorf("writes = %d, want 1", store.updates)
	}
}

// listerFunc adapts a function to ActiveUserLister.
type listerFunc func(context.Context) ([]*domain.User, error)

func (f listerFunc) ListActiveNonBannedUsers(ctx context.Context) ([]*domain.User, error) {
	return f(ctx)
}

func TestTruncateDisplayName(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		want      string
		truncated bool
	}{
		{"short name is untouched", "Ada Lovelace", "Ada Lovelace", false},
		{
			name: "exactly at the limit is untouched",
			in:   strings.Repeat("a", maxDisplayNameLength),
			want: strings.Repeat("a", maxDisplayNameLength),
		},
		{
			name:      "one byte over is cut",
			in:        strings.Repeat("a", maxDisplayNameLength+1),
			want:      strings.Repeat("a", maxDisplayNameLength),
			truncated: true,
		},
		{
			// maxDisplayNameLength counts bytes and 100 is not a multiple of 3,
			// so cutting at the byte limit would land mid-rune and store a
			// replacement character at the end of somebody's name.
			name:      "multi-byte runes are not split",
			in:        strings.Repeat("日", 40),
			want:      strings.Repeat("日", 33),
			truncated: true,
		},
		{
			name:      "trailing space left by the cut is removed",
			in:        strings.Repeat("a", maxDisplayNameLength-1) + "  tail",
			want:      strings.Repeat("a", maxDisplayNameLength-1),
			truncated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, truncated := truncateDisplayName(tt.in)
			if got != tt.want {
				t.Errorf("truncateDisplayName() = %q, want %q", got, tt.want)
			}
			if truncated != tt.truncated {
				t.Errorf("truncated = %v, want %v", truncated, tt.truncated)
			}
			if len(got) > maxDisplayNameLength {
				t.Errorf("length = %d, want at most %d — UpdateProfile would reject it", len(got), maxDisplayNameLength)
			}
			if !utf8.ValidString(got) {
				t.Errorf("result %q is not valid UTF-8", got)
			}
		})
	}
}

// Whatever truncateDisplayName returns has to survive the validation the
// backfill then puts it through, or the truncation has only moved the failure.
func TestTruncateDisplayName_ResultPassesUpdateProfile(t *testing.T) {
	store := newBackfillStore(&domain.User{ID: "u1", DisplayName: "old"})
	svc := NewUserService(store, nil)

	name, _ := truncateDisplayName(strings.Repeat("日", 40))
	if _, err := svc.UpdateProfile(context.Background(), "u1", name, "", ""); err != nil {
		t.Fatalf("UpdateProfile(%q) rejected a truncated name: %v", name, err)
	}
}

# User Sync Service Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create a UserService that syncs Kratos identities to local users via FindOrCreate, provides user lookup by ID, and supports profile updates — satisfying the `middleware.UserFinder` interface so the auth middleware can auto-provision users on first login.

**Architecture:** Single service file at `internal/service/user.go` following the existing PostService pattern: defines its own `UserRepository` interface using domain types only, uses functional options for clock injection, and generates UUIDv7 IDs. The service implements `middleware.UserFinder` by delegating `FindByKratosID` to `FindOrCreate`. Tests use an in-memory mock repository in the same package.

**Tech Stack:** Go stdlib `testing`, `github.com/google/uuid` (UUIDv7), `github.com/fireynis/the-bell/internal/domain`

**Beads Task:** `the-bell-zhd.14`

---

### Task 1: UserService with FindOrCreate

**Files:**
- Create: `internal/service/user.go`
- Create: `internal/service/user_test.go`

**Step 1: Write the failing test**

Create `internal/service/user_test.go`:

```go
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// mockUserRepo is an in-memory UserRepository for testing.
type mockUserRepo struct {
	users       map[string]*domain.User // keyed by ID
	byKratosID  map[string]string       // kratos_identity_id → user ID
	createErr   error
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{
		users:      make(map[string]*domain.User),
		byKratosID: make(map[string]string),
	}
}

func (m *mockUserRepo) GetUserByKratosID(_ context.Context, kratosID string) (*domain.User, error) {
	id, ok := m.byKratosID[kratosID]
	if !ok {
		return nil, ErrNotFound
	}
	return m.users[id], nil
}

func (m *mockUserRepo) GetUserByID(_ context.Context, id string) (*domain.User, error) {
	u, ok := m.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	return u, nil
}

func (m *mockUserRepo) CreateUser(_ context.Context, user *domain.User) (*domain.User, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	m.users[user.ID] = user
	m.byKratosID[user.KratosIdentityID] = user.ID
	return user, nil
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

func TestUserService_FindOrCreate_ExistingUser(t *testing.T) {
	repo := newMockUserRepo()
	svc := NewUserService(repo)

	existing := &domain.User{
		ID:               "user-1",
		KratosIdentityID: "kratos-abc",
		DisplayName:      "Alice",
		Role:             domain.RoleMember,
		TrustScore:       75.0,
		IsActive:         true,
	}
	repo.users["user-1"] = existing
	repo.byKratosID["kratos-abc"] = "user-1"

	got, err := svc.FindOrCreate(context.Background(), "kratos-abc")
	if err != nil {
		t.Fatalf("FindOrCreate() unexpected error: %v", err)
	}
	if got.ID != "user-1" {
		t.Errorf("ID = %q, want %q", got.ID, "user-1")
	}
	if got.TrustScore != 75.0 {
		t.Errorf("TrustScore = %v, want 75.0 (should not reset)", got.TrustScore)
	}
}

func TestUserService_FindOrCreate_NewUser(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockUserRepo()
	svc := NewUserService(repo, WithUserClock(func() time.Time { return now }))

	got, err := svc.FindOrCreate(context.Background(), "kratos-new")
	if err != nil {
		t.Fatalf("FindOrCreate() unexpected error: %v", err)
	}
	if got.ID == "" {
		t.Error("FindOrCreate() returned empty ID")
	}
	if got.KratosIdentityID != "kratos-new" {
		t.Errorf("KratosIdentityID = %q, want %q", got.KratosIdentityID, "kratos-new")
	}
	if got.Role != domain.RolePending {
		t.Errorf("Role = %q, want %q", got.Role, domain.RolePending)
	}
	if got.TrustScore != 50.0 {
		t.Errorf("TrustScore = %v, want 50.0", got.TrustScore)
	}
	if !got.IsActive {
		t.Error("IsActive = false, want true")
	}
	if !got.JoinedAt.Equal(now) {
		t.Errorf("JoinedAt = %v, want %v", got.JoinedAt, now)
	}
	if !got.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, now)
	}

	// Verify user was stored
	if _, ok := repo.users[got.ID]; !ok {
		t.Error("user not stored in repository")
	}
}

func TestUserService_FindOrCreate_CreateError(t *testing.T) {
	repo := newMockUserRepo()
	repo.createErr = errors.New("db write failed")
	svc := NewUserService(repo)

	_, err := svc.FindOrCreate(context.Background(), "kratos-fail")
	if err == nil {
		t.Fatal("FindOrCreate() expected error, got nil")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/service/ -v -run TestUserService_FindOrCreate`
Expected: FAIL — `NewUserService`, `WithUserClock`, and `FindOrCreate` don't exist yet.

**Step 3: Write minimal implementation**

Create `internal/service/user.go`:

```go
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/google/uuid"
)

// UserRepository abstracts user persistence using domain types.
type UserRepository interface {
	GetUserByKratosID(ctx context.Context, kratosID string) (*domain.User, error)
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
	CreateUser(ctx context.Context, user *domain.User) (*domain.User, error)
	UpdateUserProfile(ctx context.Context, id, displayName, bio, avatarURL string) (*domain.User, error)
}

// UserService orchestrates user business logic.
type UserService struct {
	repo UserRepository
	now  func() time.Time
}

type UserServiceOption func(*UserService)

// WithUserClock overrides the clock used by UserService. Useful for testing.
func WithUserClock(fn func() time.Time) UserServiceOption {
	return func(s *UserService) {
		s.now = fn
	}
}

func NewUserService(repo UserRepository, opts ...UserServiceOption) *UserService {
	s := &UserService{
		repo: repo,
		now:  time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// FindOrCreate looks up a user by Kratos identity ID. If no user exists,
// it creates a new pending user with the default trust score of 50.0.
func (s *UserService) FindOrCreate(ctx context.Context, kratosIdentityID string) (*domain.User, error) {
	user, err := s.repo.GetUserByKratosID(ctx, kratosIdentityID)
	if err == nil {
		return user, nil
	}
	if err != ErrNotFound {
		return nil, fmt.Errorf("looking up user by kratos id: %w", err)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generating user id: %w", err)
	}

	now := s.now()
	newUser := &domain.User{
		ID:               id.String(),
		KratosIdentityID: kratosIdentityID,
		TrustScore:       50.0,
		Role:             domain.RolePending,
		IsActive:         true,
		JoinedAt:         now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	created, err := s.repo.CreateUser(ctx, newUser)
	if err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}

	return created, nil
}
```

Notes for the implementer:
- The `err != ErrNotFound` check uses direct comparison, matching the pattern from `mockPostRepo` where `ErrNotFound` is returned directly (not wrapped). Repository implementations must return `ErrNotFound` directly for not-found cases.
- Default trust score of 50.0 matches the DB column default in `migrations/00002_create_users.sql`.
- `domain.RolePending` is the initial role — users need a vouch to become `member`.
- `JoinedAt`, `CreatedAt`, `UpdatedAt` all set to `now` on creation.

**Step 4: Run test to verify it passes**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/service/ -v -run TestUserService_FindOrCreate`
Expected: PASS — all 3 FindOrCreate tests pass.

**Step 5: Commit**

```bash
cd /home/jeremy/services/the-bell
git add internal/service/user.go internal/service/user_test.go
git commit -m "feat: add UserService with FindOrCreate for Kratos user sync"
```

---

### Task 2: UserService GetByID

**Files:**
- Modify: `internal/service/user.go`
- Modify: `internal/service/user_test.go`

**Step 1: Write the failing test**

Append to `internal/service/user_test.go`:

```go
func TestUserService_GetByID(t *testing.T) {
	repo := newMockUserRepo()
	svc := NewUserService(repo)

	existing := &domain.User{
		ID:               "user-1",
		KratosIdentityID: "kratos-abc",
		DisplayName:      "Alice",
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
```

**Step 2: Run test to verify it fails**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/service/ -v -run TestUserService_GetByID`
Expected: FAIL — `GetByID` method doesn't exist on `UserService`.

**Step 3: Write minimal implementation**

Append to `internal/service/user.go`:

```go
// GetByID retrieves a user by their internal ID.
func (s *UserService) GetByID(ctx context.Context, id string) (*domain.User, error) {
	return s.repo.GetUserByID(ctx, id)
}
```

**Step 4: Run test to verify it passes**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/service/ -v -run TestUserService_GetByID`
Expected: PASS.

**Step 5: Commit**

```bash
cd /home/jeremy/services/the-bell
git add internal/service/user.go internal/service/user_test.go
git commit -m "feat: add UserService.GetByID"
```

---

### Task 3: UserService UpdateProfile

**Files:**
- Modify: `internal/service/user.go`
- Modify: `internal/service/user_test.go`

**Step 1: Write the failing test**

Append to `internal/service/user_test.go`:

```go
func TestUserService_UpdateProfile(t *testing.T) {
	repo := newMockUserRepo()
	svc := NewUserService(repo)

	existing := &domain.User{
		ID:               "user-1",
		KratosIdentityID: "kratos-abc",
		DisplayName:      "Alice",
		Bio:              "old bio",
		AvatarURL:        "old.jpg",
		Role:             domain.RoleMember,
	}
	repo.users["user-1"] = existing

	tests := []struct {
		name        string
		id          string
		displayName string
		bio         string
		avatarURL   string
		wantErr     error
	}{
		{
			name:        "update all fields",
			id:          "user-1",
			displayName: "Alice B.",
			bio:         "new bio",
			avatarURL:   "new.jpg",
		},
		{
			name:        "not found",
			id:          "user-999",
			displayName: "Ghost",
			bio:         "",
			avatarURL:   "",
			wantErr:     ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := svc.UpdateProfile(context.Background(), tt.id, tt.displayName, tt.bio, tt.avatarURL)
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
```

**Step 2: Run test to verify it fails**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/service/ -v -run TestUserService_UpdateProfile`
Expected: FAIL — `UpdateProfile` method doesn't exist.

**Step 3: Write minimal implementation**

Append to `internal/service/user.go`:

```go
// UpdateProfile updates a user's display name, bio, and avatar URL.
func (s *UserService) UpdateProfile(ctx context.Context, id, displayName, bio, avatarURL string) (*domain.User, error) {
	return s.repo.UpdateUserProfile(ctx, id, displayName, bio, avatarURL)
}
```

**Step 4: Run test to verify it passes**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/service/ -v -run TestUserService_UpdateProfile`
Expected: PASS.

**Step 5: Commit**

```bash
cd /home/jeremy/services/the-bell
git add internal/service/user.go internal/service/user_test.go
git commit -m "feat: add UserService.UpdateProfile"
```

---

### Task 4: Implement middleware.UserFinder interface

**Files:**
- Modify: `internal/service/user.go`
- Modify: `internal/service/user_test.go`

**Step 1: Write the failing test**

Append to `internal/service/user_test.go`:

```go
func TestUserService_FindByKratosID_ImplementsUserFinder(t *testing.T) {
	repo := newMockUserRepo()
	svc := NewUserService(repo)

	existing := &domain.User{
		ID:               "user-1",
		KratosIdentityID: "kratos-xyz",
		Role:             domain.RoleMember,
		IsActive:         true,
	}
	repo.users["user-1"] = existing
	repo.byKratosID["kratos-xyz"] = "user-1"

	// FindByKratosID delegates to FindOrCreate
	got, err := svc.FindByKratosID(context.Background(), "kratos-xyz")
	if err != nil {
		t.Fatalf("FindByKratosID() unexpected error: %v", err)
	}
	if got.ID != "user-1" {
		t.Errorf("ID = %q, want %q", got.ID, "user-1")
	}
}

func TestUserService_FindByKratosID_CreatesIfMissing(t *testing.T) {
	repo := newMockUserRepo()
	svc := NewUserService(repo)

	got, err := svc.FindByKratosID(context.Background(), "kratos-brand-new")
	if err != nil {
		t.Fatalf("FindByKratosID() unexpected error: %v", err)
	}
	if got.Role != domain.RolePending {
		t.Errorf("Role = %q, want %q (auto-created user should be pending)", got.Role, domain.RolePending)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/service/ -v -run TestUserService_FindByKratosID`
Expected: FAIL — `FindByKratosID` method doesn't exist.

**Step 3: Write minimal implementation**

Append to `internal/service/user.go`:

```go
// FindByKratosID satisfies middleware.UserFinder. It delegates to FindOrCreate
// so that first-time Kratos users are auto-provisioned as pending.
func (s *UserService) FindByKratosID(ctx context.Context, kratosID string) (*domain.User, error) {
	return s.FindOrCreate(ctx, kratosID)
}
```

**Step 4: Add compile-time interface check**

Add this line near the top of `internal/service/user.go` (after the imports, before UserRepository):

```go
// Compile-time check: UserService must satisfy middleware.UserFinder.
var _ interface {
	FindByKratosID(ctx context.Context, kratosID string) (*domain.User, error)
} = (*UserService)(nil)
```

Notes for the implementer:
- We use a structural interface check rather than importing the `middleware` package to avoid a circular dependency (middleware imports domain, service imports domain — adding service→middleware would be fine directionally, but keeping service decoupled from middleware is better architecture).
- The structural check verifies the method signature matches at compile time.

**Step 5: Run all tests to verify nothing is broken**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/service/ -v`
Expected: All user and post service tests pass.

**Step 6: Verify build**

Run: `cd /home/jeremy/services/the-bell && go build ./...`
Expected: Clean build, no errors.

**Step 7: Commit**

```bash
cd /home/jeremy/services/the-bell
git add internal/service/user.go internal/service/user_test.go
git commit -m "feat: implement FindByKratosID for middleware.UserFinder interface"
```

---

### Task 5: Run full test suite and verify

**Step 1: Run all tests**

Run: `cd /home/jeremy/services/the-bell && go test ./... -v`
Expected: All tests pass across all packages (domain, service, handler, middleware, database).

**Step 2: Run go vet**

Run: `cd /home/jeremy/services/the-bell && go vet ./...`
Expected: No issues.

---

## Edge Cases and Risks

1. **Race condition in FindOrCreate**: Two concurrent requests for the same Kratos ID could both see `ErrNotFound` and attempt to create. The `kratos_identity_id` column has a `UNIQUE` constraint (`migrations/00002_create_users.sql`), so the second `CreateUser` will fail with a unique violation. The service does NOT handle this — the repository adapter (future task) should catch the unique violation and retry the lookup. For now, the service layer correctly delegates to the repository, and the mock doesn't simulate races.

2. **ErrNotFound comparison**: The service uses `err != ErrNotFound` (direct comparison, not `errors.Is`). This matches the mock pattern from `post_test.go`. Repository implementations must return `ErrNotFound` directly, not wrapped. If a future adapter wraps errors, this comparison will break — the adapter must return the sentinel directly.

3. **Empty kratosIdentityID**: The service doesn't validate that `kratosIdentityID` is non-empty. The DB column is `NOT NULL` with a `UNIQUE` constraint, so an empty string would be accepted by the DB but could cause issues. This is not validated here because: (a) the Kratos middleware always provides a real ID from the session, and (b) YAGNI — validation at the system boundary (middleware) is sufficient.

4. **UpdateProfile with empty strings**: The service passes empty strings through to the repository. This is intentional — a user clearing their bio or avatar URL should be allowed. The DB columns default to `''`.

5. **Middleware integration**: The auth middleware (`internal/middleware/auth.go:55-66`) calls `finder.FindByKratosID` and checks `user == nil` separately from `err != nil`. Since `FindOrCreate` always returns a non-nil user on success (it creates one if missing), the `user == nil` branch in the middleware becomes unreachable when using `UserService`. This is fine — the middleware's nil check is a safety net for other `UserFinder` implementations.

## Test Strategy

| Test | Type | What it validates |
|------|------|-------------------|
| `TestUserService_FindOrCreate_ExistingUser` | Unit | Returns existing user without modification |
| `TestUserService_FindOrCreate_NewUser` | Unit | Creates pending user with trust 50.0, correct timestamps |
| `TestUserService_FindOrCreate_CreateError` | Unit | Propagates repository create errors |
| `TestUserService_GetByID` | Unit | Lookup by ID, returns ErrNotFound for missing |
| `TestUserService_UpdateProfile` | Unit | Updates display name/bio/avatar, ErrNotFound for missing |
| `TestUserService_FindByKratosID_ImplementsUserFinder` | Unit | Delegates to FindOrCreate for existing users |
| `TestUserService_FindByKratosID_CreatesIfMissing` | Unit | Auto-provisions pending user via FindOrCreate |
| Compile-time interface check | Build | UserService satisfies `FindByKratosID` signature |
| `go test ./...` | All | Full suite remains green |
| `go vet ./...` | Lint | No static analysis issues |

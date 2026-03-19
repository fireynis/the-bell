# Council User Approval (Bootstrap Phase) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add API endpoints for council members to list pending users and approve them during bootstrap mode, with automatic exit from bootstrap mode when the member count reaches 20.

**Architecture:** New `ApprovalService` in `internal/service/approval.go` depends on `UserRepository` (extended with `ListPendingUsers` and `CountUsersByMinRole`), `ConfigRepository`, and `UserGetter` (for role updates). A new `ApprovalHandler` in `internal/handler/approval.go` exposes two endpoints behind `RequireRole(RoleCouncil)`. The service checks `bootstrap_mode=true` before allowing operations, promotes pending users to member, and checks the member threshold to auto-disable bootstrap mode. New sqlc queries: `ListPendingUsers` and `CountUsersByMinRole`.

**Tech Stack:** Go, chi router, sqlc, pgx/v5, goose migrations (no new migrations needed — existing schema supports this)

---

### Task 1: Add sqlc queries for pending users and member count

**Files:**
- Modify: `queries/users.sql`

**Step 1: Write the new SQL queries**

Add to end of `queries/users.sql`:

```sql
-- name: ListPendingUsers :many
SELECT * FROM users
WHERE role = 'pending' AND is_active = TRUE
ORDER BY created_at ASC;

-- name: CountUsersByMinRole :one
SELECT COUNT(*) FROM users
WHERE role IN ('member', 'moderator', 'council') AND is_active = TRUE;
```

**Step 2: Run sqlc generate**

Run: `sqlc generate`
Expected: New functions in `internal/repository/postgres/users.sql.go`

**Step 3: Verify build compiles**

Run: `go build ./...`
Expected: PASS

**Step 4: Commit**

```bash
git add queries/users.sql internal/repository/postgres/users.sql.go
git commit -m "feat: add sqlc queries for pending users and member count"
```

---

### Task 2: Extend UserRepository interface and UserRepo adapter

**Files:**
- Modify: `internal/service/user.go` (add methods to `UserRepository` interface)
- Modify: `internal/repository/postgres/user_repo.go` (implement new methods)

**Step 1: Add methods to UserRepository interface**

In `internal/service/user.go`, add to the `UserRepository` interface:

```go
ListPendingUsers(ctx context.Context) ([]*domain.User, error)
CountActiveMembers(ctx context.Context) (int64, error)
```

**Step 2: Implement in UserRepo adapter**

In `internal/repository/postgres/user_repo.go`, add:

```go
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

func (r *UserRepo) CountActiveMembers(ctx context.Context) (int64, error) {
	return r.q.CountUsersByMinRole(ctx)
}
```

**Step 3: Verify build compiles**

Run: `go build ./...`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/service/user.go internal/repository/postgres/user_repo.go
git commit -m "feat: extend UserRepository with pending users and member count"
```

---

### Task 3: Update mock UserRepo in tests

**Files:**
- Modify: `internal/service/user_test.go` (add methods to `mockUserRepo`)

**Step 1: Add mock implementations**

In `internal/service/user_test.go`, add methods to `mockUserRepo`:

```go
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
```

**Step 2: Run existing tests to verify no breakage**

Run: `go test ./internal/service/ -v -run TestUserService`
Expected: All existing user tests PASS

**Step 3: Commit**

```bash
git add internal/service/user_test.go
git commit -m "test: add mock methods for ListPendingUsers and CountActiveMembers"
```

---

### Task 4: Write ApprovalService with failing tests

**Files:**
- Create: `internal/service/approval.go`
- Create: `internal/service/approval_test.go`

**Step 1: Write the failing tests**

Create `internal/service/approval_test.go`:

```go
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// mockApprovalUserRepo extends mockUserRepo with role update support.
type mockApprovalUserRepo struct {
	*mockUserRepo
	updateRoleErr error
	updatedRoles  map[string]domain.Role
}

func newMockApprovalUserRepo() *mockApprovalUserRepo {
	return &mockApprovalUserRepo{
		mockUserRepo: newMockUserRepo(),
		updatedRoles: make(map[string]domain.Role),
	}
}

func (m *mockApprovalUserRepo) UpdateUserRole(_ context.Context, id string, role domain.Role) error {
	if m.updateRoleErr != nil {
		return m.updateRoleErr
	}
	u, ok := m.users[id]
	if !ok {
		return ErrNotFound
	}
	u.Role = role
	m.updatedRoles[id] = role
	return nil
}

var approvalFixedNow = time.Date(2026, 3, 1, 14, 0, 0, 0, time.UTC)

func approvalFixedClock() time.Time { return approvalFixedNow }

// --- ListPending ---

func TestApprovalService_ListPending_Success(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "true"
	userRepo.users["pending-1"] = &domain.User{
		ID: "pending-1", Role: domain.RolePending, IsActive: true,
		CreatedAt: approvalFixedNow,
	}
	userRepo.users["member-1"] = &domain.User{
		ID: "member-1", Role: domain.RoleMember, IsActive: true,
	}

	svc := NewApprovalService(userRepo, configRepo)
	users, err := svc.ListPending(context.Background())
	if err != nil {
		t.Fatalf("ListPending() unexpected error: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("ListPending() returned %d users, want 1", len(users))
	}
	if users[0].ID != "pending-1" {
		t.Errorf("ListPending()[0].ID = %q, want %q", users[0].ID, "pending-1")
	}
}

func TestApprovalService_ListPending_NotBootstrapMode(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "false"

	svc := NewApprovalService(userRepo, configRepo)
	_, err := svc.ListPending(context.Background())
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("ListPending() error = %v, want %v", err, ErrForbidden)
	}
}

func TestApprovalService_ListPending_NoBootstrapKey(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo() // empty config, key not found

	svc := NewApprovalService(userRepo, configRepo)
	_, err := svc.ListPending(context.Background())
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("ListPending() error = %v, want %v", err, ErrForbidden)
	}
}

// --- Approve ---

func TestApprovalService_Approve_Success(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "true"
	userRepo.users["pending-1"] = &domain.User{
		ID: "pending-1", Role: domain.RolePending, IsActive: true,
	}

	svc := NewApprovalService(userRepo, configRepo)
	user, err := svc.Approve(context.Background(), "pending-1")
	if err != nil {
		t.Fatalf("Approve() unexpected error: %v", err)
	}
	if user.Role != domain.RoleMember {
		t.Errorf("user.Role = %q, want %q", user.Role, domain.RoleMember)
	}
	if userRepo.updatedRoles["pending-1"] != domain.RoleMember {
		t.Error("UpdateUserRole not called")
	}
}

func TestApprovalService_Approve_NotBootstrapMode(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "false"

	svc := NewApprovalService(userRepo, configRepo)
	_, err := svc.Approve(context.Background(), "pending-1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Approve() error = %v, want %v", err, ErrForbidden)
	}
}

func TestApprovalService_Approve_UserNotFound(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "true"

	svc := NewApprovalService(userRepo, configRepo)
	_, err := svc.Approve(context.Background(), "nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Approve() error = %v, want %v", err, ErrNotFound)
	}
}

func TestApprovalService_Approve_NotPending(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "true"
	userRepo.users["member-1"] = &domain.User{
		ID: "member-1", Role: domain.RoleMember, IsActive: true,
	}

	svc := NewApprovalService(userRepo, configRepo)
	_, err := svc.Approve(context.Background(), "member-1")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Approve() error = %v, want %v", err, ErrValidation)
	}
}

func TestApprovalService_Approve_InactiveUser(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "true"
	userRepo.users["inactive-1"] = &domain.User{
		ID: "inactive-1", Role: domain.RolePending, IsActive: false,
	}

	svc := NewApprovalService(userRepo, configRepo)
	_, err := svc.Approve(context.Background(), "inactive-1")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Approve() error = %v, want %v", err, ErrValidation)
	}
}

func TestApprovalService_Approve_ExitsBootstrapAt20Members(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "true"

	// Add 19 existing active members (council + members)
	for i := 0; i < 19; i++ {
		id := fmt.Sprintf("member-%d", i)
		userRepo.users[id] = &domain.User{
			ID: id, Role: domain.RoleMember, IsActive: true,
		}
	}
	// Add the pending user who will become #20
	userRepo.users["pending-1"] = &domain.User{
		ID: "pending-1", Role: domain.RolePending, IsActive: true,
	}

	svc := NewApprovalService(userRepo, configRepo)
	_, err := svc.Approve(context.Background(), "pending-1")
	if err != nil {
		t.Fatalf("Approve() unexpected error: %v", err)
	}

	if configRepo.config["bootstrap_mode"] != "false" {
		t.Errorf("bootstrap_mode = %q, want %q", configRepo.config["bootstrap_mode"], "false")
	}
}

func TestApprovalService_Approve_StaysBootstrapBelow20(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "true"

	// Add 5 existing active members
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("member-%d", i)
		userRepo.users[id] = &domain.User{
			ID: id, Role: domain.RoleMember, IsActive: true,
		}
	}
	userRepo.users["pending-1"] = &domain.User{
		ID: "pending-1", Role: domain.RolePending, IsActive: true,
	}

	svc := NewApprovalService(userRepo, configRepo)
	_, err := svc.Approve(context.Background(), "pending-1")
	if err != nil {
		t.Fatalf("Approve() unexpected error: %v", err)
	}

	if configRepo.config["bootstrap_mode"] != "true" {
		t.Errorf("bootstrap_mode = %q, want %q (should remain true below threshold)", configRepo.config["bootstrap_mode"], "true")
	}
}

func TestApprovalService_Approve_RoleUpdateError(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	userRepo.updateRoleErr = errors.New("db write failed")
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "true"
	userRepo.users["pending-1"] = &domain.User{
		ID: "pending-1", Role: domain.RolePending, IsActive: true,
	}

	svc := NewApprovalService(userRepo, configRepo)
	_, err := svc.Approve(context.Background(), "pending-1")
	if err == nil {
		t.Fatal("Approve() expected error, got nil")
	}
}
```

Note: You'll need to add `"fmt"` to the import list for the threshold test.

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/service/ -v -run TestApprovalService`
Expected: FAIL — `NewApprovalService` undefined

**Step 3: Commit**

```bash
git add internal/service/approval_test.go
git commit -m "test: add failing tests for ApprovalService"
```

---

### Task 5: Implement ApprovalService

**Files:**
- Create: `internal/service/approval.go`

**Step 1: Write minimal implementation**

Create `internal/service/approval.go`:

```go
package service

import (
	"context"
	"fmt"

	"github.com/fireynis/the-bell/internal/domain"
)

const bootstrapExitThreshold = 20

// ApprovalUserRepository combines user lookup, listing, counting, and role updates.
type ApprovalUserRepository interface {
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
	ListPendingUsers(ctx context.Context) ([]*domain.User, error)
	CountActiveMembers(ctx context.Context) (int64, error)
	UpdateUserRole(ctx context.Context, id string, role domain.Role) error
}

// ApprovalService handles council approval of pending users during bootstrap.
type ApprovalService struct {
	users  ApprovalUserRepository
	config ConfigRepository
}

func NewApprovalService(users ApprovalUserRepository, config ConfigRepository) *ApprovalService {
	return &ApprovalService{
		users:  users,
		config: config,
	}
}

// ListPending returns all pending users. Only available during bootstrap mode.
func (s *ApprovalService) ListPending(ctx context.Context) ([]*domain.User, error) {
	if err := s.requireBootstrap(ctx); err != nil {
		return nil, err
	}
	return s.users.ListPendingUsers(ctx)
}

// Approve promotes a pending user to member. Only available during bootstrap mode.
// When the active member count reaches the threshold, bootstrap mode is auto-disabled.
func (s *ApprovalService) Approve(ctx context.Context, userID string) (*domain.User, error) {
	if err := s.requireBootstrap(ctx); err != nil {
		return nil, err
	}

	user, err := s.users.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("looking up user: %w", err)
	}

	if user.Role != domain.RolePending {
		return nil, fmt.Errorf("%w: user is not pending", ErrValidation)
	}
	if !user.IsActive {
		return nil, fmt.Errorf("%w: user is not active", ErrValidation)
	}

	if err := s.users.UpdateUserRole(ctx, userID, domain.RoleMember); err != nil {
		return nil, fmt.Errorf("updating user role: %w", err)
	}
	user.Role = domain.RoleMember

	count, err := s.users.CountActiveMembers(ctx)
	if err != nil {
		return user, nil // approval succeeded; count check is best-effort
	}
	if count >= bootstrapExitThreshold {
		_ = s.config.SetTownConfig(ctx, "bootstrap_mode", "false")
	}

	return user, nil
}

func (s *ApprovalService) requireBootstrap(ctx context.Context) error {
	val, err := s.config.GetTownConfig(ctx, "bootstrap_mode")
	if err != nil {
		return fmt.Errorf("%w: bootstrap mode not available", ErrForbidden)
	}
	if val != "true" {
		return fmt.Errorf("%w: not in bootstrap mode", ErrForbidden)
	}
	return nil
}
```

**Step 2: Run the tests to verify they pass**

Run: `go test ./internal/service/ -v -run TestApprovalService`
Expected: All PASS

**Step 3: Run all tests to check for regressions**

Run: `go test ./...`
Expected: All PASS

**Step 4: Commit**

```bash
git add internal/service/approval.go
git commit -m "feat: add ApprovalService for council user approval"
```

---

### Task 6: Write ApprovalHandler with failing tests

**Files:**
- Create: `internal/handler/approval.go`
- Create: `internal/handler/approval_test.go`

**Step 1: Write the failing handler tests**

Create `internal/handler/approval_test.go`:

```go
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/service"
)

// mockApprovalService implements the interface needed by ApprovalHandler.
type mockApprovalService struct {
	pendingUsers []*domain.User
	approvedUser *domain.User
	listErr      error
	approveErr   error
}

func (m *mockApprovalService) ListPending(_ context.Context) ([]*domain.User, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.pendingUsers, nil
}

func (m *mockApprovalService) Approve(_ context.Context, userID string) (*domain.User, error) {
	if m.approveErr != nil {
		return nil, m.approveErr
	}
	return m.approvedUser, nil
}

func TestApprovalHandler_ListPending_Success(t *testing.T) {
	svc := &mockApprovalService{
		pendingUsers: []*domain.User{
			{ID: "user-1", DisplayName: "Alice", Role: domain.RolePending},
		},
	}
	h := NewApprovalHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vouches/pending", nil)
	ctx := middleware.WithUser(req.Context(), &domain.User{
		ID: "council-1", Role: domain.RoleCouncil, IsActive: true,
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ListPending(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Users []domain.User `json:"users"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(resp.Users) != 1 {
		t.Fatalf("len(users) = %d, want 1", len(resp.Users))
	}
	if resp.Users[0].ID != "user-1" {
		t.Errorf("users[0].ID = %q, want %q", resp.Users[0].ID, "user-1")
	}
}

func TestApprovalHandler_ListPending_EmptyList(t *testing.T) {
	svc := &mockApprovalService{pendingUsers: nil}
	h := NewApprovalHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vouches/pending", nil)
	ctx := middleware.WithUser(req.Context(), &domain.User{
		ID: "council-1", Role: domain.RoleCouncil, IsActive: true,
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ListPending(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Users []domain.User `json:"users"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Users == nil || len(resp.Users) != 0 {
		t.Errorf("expected empty array, got %v", resp.Users)
	}
}

func TestApprovalHandler_ListPending_ServiceError(t *testing.T) {
	svc := &mockApprovalService{
		listErr: service.ErrForbidden,
	}
	h := NewApprovalHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vouches/pending", nil)
	ctx := middleware.WithUser(req.Context(), &domain.User{
		ID: "council-1", Role: domain.RoleCouncil, IsActive: true,
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ListPending(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestApprovalHandler_Approve_Success(t *testing.T) {
	svc := &mockApprovalService{
		approvedUser: &domain.User{
			ID: "user-1", DisplayName: "Alice", Role: domain.RoleMember,
		},
	}
	h := NewApprovalHandler(svc)

	r := chi.NewRouter()
	r.Post("/api/v1/vouches/approve/{id}", func(w http.ResponseWriter, req *http.Request) {
		ctx := middleware.WithUser(req.Context(), &domain.User{
			ID: "council-1", Role: domain.RoleCouncil, IsActive: true,
		})
		h.Approve(w, req.WithContext(ctx))
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/vouches/approve/user-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp domain.User
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Role != domain.RoleMember {
		t.Errorf("role = %q, want %q", resp.Role, domain.RoleMember)
	}
}

func TestApprovalHandler_Approve_NotFound(t *testing.T) {
	svc := &mockApprovalService{
		approveErr: service.ErrNotFound,
	}
	h := NewApprovalHandler(svc)

	r := chi.NewRouter()
	r.Post("/api/v1/vouches/approve/{id}", func(w http.ResponseWriter, req *http.Request) {
		ctx := middleware.WithUser(req.Context(), &domain.User{
			ID: "council-1", Role: domain.RoleCouncil, IsActive: true,
		})
		h.Approve(w, req.WithContext(ctx))
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/vouches/approve/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestApprovalHandler_Approve_Unauthorized(t *testing.T) {
	svc := &mockApprovalService{}
	h := NewApprovalHandler(svc)

	// No user in context
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vouches/approve/user-1", nil)
	w := httptest.NewRecorder()

	h.Approve(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/handler/ -v -run TestApprovalHandler`
Expected: FAIL — `NewApprovalHandler` undefined

**Step 3: Commit**

```bash
git add internal/handler/approval_test.go
git commit -m "test: add failing tests for ApprovalHandler"
```

---

### Task 7: Implement ApprovalHandler

**Files:**
- Create: `internal/handler/approval.go`

**Step 1: Write the handler implementation**

Create `internal/handler/approval.go`:

```go
package handler

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
)

// ApprovalLister lists pending users and approves them.
type ApprovalLister interface {
	ListPending(ctx context.Context) ([]*domain.User, error)
	Approve(ctx context.Context, userID string) (*domain.User, error)
}

// ApprovalHandler handles HTTP requests for council user approval.
type ApprovalHandler struct {
	approvals ApprovalLister
}

// NewApprovalHandler creates an ApprovalHandler.
func NewApprovalHandler(approvals ApprovalLister) *ApprovalHandler {
	return &ApprovalHandler{approvals: approvals}
}

type listPendingResponse struct {
	Users []*domain.User `json:"users"`
}

// ListPending handles GET /api/v1/vouches/pending.
func (h *ApprovalHandler) ListPending(w http.ResponseWriter, r *http.Request) {
	users, err := h.approvals.ListPending(r.Context())
	if err != nil {
		serviceError(w, err)
		return
	}

	if users == nil {
		users = []*domain.User{}
	}

	JSON(w, http.StatusOK, listPendingResponse{Users: users})
}

// Approve handles POST /api/v1/vouches/approve/{id}.
func (h *ApprovalHandler) Approve(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	userID := chi.URLParam(r, "id")

	user, err := h.approvals.Approve(r.Context(), userID)
	if err != nil {
		serviceError(w, err)
		return
	}

	JSON(w, http.StatusOK, user)
}
```

**Step 2: Run handler tests to verify they pass**

Run: `go test ./internal/handler/ -v -run TestApprovalHandler`
Expected: All PASS

**Step 3: Run all tests**

Run: `go test ./...`
Expected: All PASS

**Step 4: Commit**

```bash
git add internal/handler/approval.go
git commit -m "feat: add ApprovalHandler for council user approval endpoints"
```

---

### Task 8: Wire routes and server

**Files:**
- Modify: `internal/server/server.go` (add `approvalService` field and `WithApprovalService` option)
- Modify: `internal/server/routes.go` (add approval routes)
- Modify: `cmd/bell/main.go` (wire service in `runServe`)

**Step 1: Add service field and option to Server**

In `internal/server/server.go`, add to the `Server` struct:

```go
approvalService *service.ApprovalService
```

Add the option function:

```go
// WithApprovalService sets the ApprovalService used by approval handlers.
func WithApprovalService(as *service.ApprovalService) Option {
	return func(s *Server) { s.approvalService = as }
}
```

**Step 2: Add routes in routes.go**

In `internal/server/routes.go`, add after the moderation route block (before `return r`):

```go
if s.approvalService != nil {
	ah := handler.NewApprovalHandler(s.approvalService)
	r.Route("/api/v1/vouches", func(r chi.Router) {
		if s.authMiddleware != nil {
			r.Use(s.authMiddleware)
		}
		r.Use(middleware.RequireActive)
		r.Use(middleware.RequireRole(domain.RoleCouncil))
		r.Get("/pending", ah.ListPending)
		r.Post("/approve/{id}", ah.Approve)
	})
}
```

**Step 3: Wire in main.go runServe**

In `cmd/bell/main.go` `runServe`, update the server construction. The `ApprovalService` needs `UserRepo` (which implements `ApprovalUserRepository`) and `ConfigRepo`. Add after `mustInit`:

```go
queries := postgres.New(pool)
userRepo := postgres.NewUserRepo(queries)
configRepo := postgres.NewConfigRepo(queries)
approvalSvc := service.NewApprovalService(userRepo, configRepo)

srv := server.New(cfg, pool, logger,
	server.WithApprovalService(approvalSvc),
)
```

Note: `UserRepo` must satisfy `ApprovalUserRepository`. It already has `GetUserByID` and `UpdateUserRole` (via the sqlc-generated code), and we added `ListPendingUsers` and `CountActiveMembers` in Task 2. However, `UserRepo` does NOT currently implement `UpdateUserRole` — that method is on `Queries` directly, not on `UserRepo`. We need to add it to `UserRepo`.

**Step 4: Add UpdateUserRole to UserRepo**

In `internal/repository/postgres/user_repo.go`, add:

```go
func (r *UserRepo) UpdateUserRole(ctx context.Context, id string, role domain.Role) error {
	return r.q.UpdateUserRole(ctx, UpdateUserRoleParams{
		ID:   id,
		Role: string(role),
	})
}
```

**Step 5: Verify build compiles**

Run: `go build ./...`
Expected: PASS

**Step 6: Run all tests**

Run: `go test ./...`
Expected: All PASS

**Step 7: Commit**

```bash
git add internal/server/server.go internal/server/routes.go internal/repository/postgres/user_repo.go cmd/bell/main.go
git commit -m "feat: wire council approval routes into server"
```

---

### Task 9: Add server-level route tests

**Files:**
- Modify: Existing server tests (if any) or create `internal/server/routes_test.go`

**Step 1: Check if server route tests exist**

Look at existing test patterns in `internal/server/`. The server tests likely test route registration. Check `internal/server/routes_test.go` or similar files. If they exist, add tests for the new routes. If not, add a basic test.

**Step 2: Write route registration test**

Create or add to `internal/server/routes_test.go`:

```go
// Verify that the /api/v1/vouches routes are registered when ApprovalService is set.
func TestRoutes_VouchesApproval_Registered(t *testing.T) {
	// Create a minimal server with a mock approval service
	// and verify the routes return non-404 responses
}
```

This is optional — the handler tests already cover the logic. If there are existing route tests, follow that pattern. Otherwise, skip this task.

**Step 3: Run all tests one final time**

Run: `go test ./...`
Expected: All PASS

**Step 4: Commit if new tests added**

```bash
git add internal/server/
git commit -m "test: add route registration tests for approval endpoints"
```

---

### Task 10: Final verification and cleanup

**Step 1: Run the full test suite**

Run: `go test ./... -v`
Expected: All PASS

**Step 2: Run go vet**

Run: `go vet ./...`
Expected: No issues

**Step 3: Run build**

Run: `go build ./...`
Expected: PASS

**Step 4: Verify API contract matches ticket**

Check the ticket requirements:
- [x] `GET /api/v1/vouches/pending` — lists pending users (council only)
- [x] `POST /api/v1/vouches/approve/:id` — council approves, sets role to member
- [x] Auto-exit bootstrap when member count >= 20

**Step 5: Final commit if any cleanup needed, then push**

```bash
git push
```

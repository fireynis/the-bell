# Moderation Audit Trail Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add GET /api/v1/moderation/actions/{user_id} endpoint that returns full moderation action history with propagated penalties, supporting moderator audit by council members.

**Architecture:** New SQL queries for listing actions-by-target, actions-by-moderator, and penalties-by-action-IDs. A new service method `GetActionHistory` composes these. A new handler method `ListActions` serves the GET endpoint. Council members can pass `?role=moderator` to view actions taken *by* a user (instead of *against*), enabling moderator behavior review.

**Tech Stack:** Go, chi router, sqlc, pgx/v5, standard library testing

---

### Task 1: Add SQL queries for audit trail

**Files:**
- Modify: `queries/moderation_actions.sql` (append new query)
- Modify: `queries/trust_penalties.sql` (append new query)

**Step 1: Add ListModerationActionsByModerator query**

Append to `queries/moderation_actions.sql`:

```sql
-- name: ListModerationActionsByModerator :many
SELECT * FROM moderation_actions
WHERE moderator_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;
```

**Step 2: Add ListTrustPenaltiesByActionID query**

Append to `queries/trust_penalties.sql`:

```sql
-- name: ListTrustPenaltiesByActionID :many
SELECT * FROM trust_penalties
WHERE moderation_action_id = $1
ORDER BY hop_depth ASC;
```

**Step 3: Run sqlc generate**

Run: `cd /home/jeremy/services/the-bell && sqlc generate`
Expected: No errors. New functions appear in `internal/repository/postgres/moderation_actions.sql.go` and `internal/repository/postgres/trust_penalties.sql.go`.

**Step 4: Verify generated code compiles**

Run: `cd /home/jeremy/services/the-bell && go build ./...`
Expected: No errors.

**Step 5: Commit**

```bash
git add queries/moderation_actions.sql queries/trust_penalties.sql internal/repository/postgres/moderation_actions.sql.go internal/repository/postgres/trust_penalties.sql.go
git commit -m "feat: add SQL queries for moderation audit trail"
```

---

### Task 2: Add service method for action history

**Files:**
- Modify: `internal/service/moderation_action.go` (add interfaces + method)
- Create: `internal/service/moderation_action_history_test.go`

**Step 1: Write the failing tests**

Create `internal/service/moderation_action_history_test.go`:

```go
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// --- mock ActionHistoryRepository ---

type mockActionHistoryRepo struct {
	actionsByTarget    []*domain.ModerationAction
	actionsByModerator []*domain.ModerationAction
	listErr            error
}

func newMockActionHistoryRepo() *mockActionHistoryRepo {
	return &mockActionHistoryRepo{}
}

func (m *mockActionHistoryRepo) CreateModerationAction(_ context.Context, action *domain.ModerationAction) error {
	return nil
}

func (m *mockActionHistoryRepo) ListActionsByTarget(_ context.Context, targetUserID string, limit, offset int) ([]*domain.ModerationAction, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.actionsByTarget, nil
}

func (m *mockActionHistoryRepo) ListActionsByModerator(_ context.Context, moderatorID string, limit, offset int) ([]*domain.ModerationAction, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.actionsByModerator, nil
}

// --- mock PenaltyLister ---

type mockPenaltyLister struct {
	penalties map[string][]domain.TrustPenalty
	listErr   error
}

func newMockPenaltyLister() *mockPenaltyLister {
	return &mockPenaltyLister{penalties: make(map[string][]domain.TrustPenalty)}
}

func (m *mockPenaltyLister) CreateTrustPenalty(_ context.Context, p *domain.TrustPenalty) error {
	return nil
}

func (m *mockPenaltyLister) ListPenaltiesByActionID(_ context.Context, actionID string) ([]domain.TrustPenalty, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.penalties[actionID], nil
}

// --- helper ---

func newTestHistoryService(
	actions ModerationActionRepository,
	penaltyLister PenaltyLister,
) *ModerationActionService {
	modSvc := NewModerationService(penaltyLister, newMockPenaltyGraph(), fixedClock)
	return NewModerationActionService(actions, newMockActionUserLookup(), modSvc, nil, fixedClock)
}

// --- GetActionHistory: by target ---

func TestModerationActionService_GetActionHistory_ByTarget(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	actionRepo := newMockActionHistoryRepo()
	actionRepo.actionsByTarget = []*domain.ModerationAction{
		{ID: "act-1", TargetUserID: "user-1", ModeratorID: "mod-1", Action: domain.ActionWarn, Severity: 1, Reason: "first", CreatedAt: now},
		{ID: "act-2", TargetUserID: "user-1", ModeratorID: "mod-1", Action: domain.ActionMute, Severity: 3, Reason: "second", CreatedAt: now},
	}

	penaltyLister := newMockPenaltyLister()
	penaltyLister.penalties["act-1"] = []domain.TrustPenalty{
		{ID: "pen-1", UserID: "user-1", ModerationActionID: "act-1", PenaltyAmount: 5.0, HopDepth: 0, CreatedAt: now},
	}
	penaltyLister.penalties["act-2"] = []domain.TrustPenalty{
		{ID: "pen-2", UserID: "user-1", ModerationActionID: "act-2", PenaltyAmount: 20.0, HopDepth: 0, CreatedAt: now},
		{ID: "pen-3", UserID: "voucher-1", ModerationActionID: "act-2", PenaltyAmount: 10.0, HopDepth: 1, CreatedAt: now},
	}

	svc := newTestHistoryService(actionRepo, penaltyLister)
	result, err := svc.GetActionHistory(context.Background(), "user-1", false, 20, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("got %d entries, want 2", len(result))
	}
	if result[0].Action.ID != "act-1" {
		t.Errorf("result[0].Action.ID = %q, want %q", result[0].Action.ID, "act-1")
	}
	if len(result[0].Penalties) != 1 {
		t.Errorf("result[0] has %d penalties, want 1", len(result[0].Penalties))
	}
	if len(result[1].Penalties) != 2 {
		t.Errorf("result[1] has %d penalties, want 2", len(result[1].Penalties))
	}
}

// --- GetActionHistory: by moderator ---

func TestModerationActionService_GetActionHistory_ByModerator(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	actionRepo := newMockActionHistoryRepo()
	actionRepo.actionsByModerator = []*domain.ModerationAction{
		{ID: "act-1", TargetUserID: "user-1", ModeratorID: "mod-1", Action: domain.ActionBan, Severity: 5, Reason: "banned", CreatedAt: now},
	}

	penaltyLister := newMockPenaltyLister()
	penaltyLister.penalties["act-1"] = []domain.TrustPenalty{
		{ID: "pen-1", UserID: "user-1", ModerationActionID: "act-1", PenaltyAmount: 50.0, HopDepth: 0, CreatedAt: now},
	}

	svc := newTestHistoryService(actionRepo, penaltyLister)
	result, err := svc.GetActionHistory(context.Background(), "mod-1", true, 20, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("got %d entries, want 1", len(result))
	}
	if result[0].Action.ModeratorID != "mod-1" {
		t.Errorf("moderator = %q, want %q", result[0].Action.ModeratorID, "mod-1")
	}
}

// --- GetActionHistory: empty result ---

func TestModerationActionService_GetActionHistory_Empty(t *testing.T) {
	actionRepo := newMockActionHistoryRepo()
	penaltyLister := newMockPenaltyLister()

	svc := newTestHistoryService(actionRepo, penaltyLister)
	result, err := svc.GetActionHistory(context.Background(), "user-1", false, 20, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(result) != 0 {
		t.Errorf("got %d entries, want 0", len(result))
	}
}

// --- GetActionHistory: repo error ---

func TestModerationActionService_GetActionHistory_RepoError(t *testing.T) {
	actionRepo := newMockActionHistoryRepo()
	actionRepo.listErr = errors.New("db down")
	penaltyLister := newMockPenaltyLister()

	svc := newTestHistoryService(actionRepo, penaltyLister)
	_, err := svc.GetActionHistory(context.Background(), "user-1", false, 20, 0)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- GetActionHistory: penalty fetch error ---

func TestModerationActionService_GetActionHistory_PenaltyError(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	actionRepo := newMockActionHistoryRepo()
	actionRepo.actionsByTarget = []*domain.ModerationAction{
		{ID: "act-1", TargetUserID: "user-1", ModeratorID: "mod-1", Action: domain.ActionWarn, Severity: 1, Reason: "test", CreatedAt: now},
	}

	penaltyLister := newMockPenaltyLister()
	penaltyLister.listErr = errors.New("penalty db down")

	svc := newTestHistoryService(actionRepo, penaltyLister)
	_, err := svc.GetActionHistory(context.Background(), "user-1", false, 20, 0)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/service/ -run TestModerationActionService_GetActionHistory -v`
Expected: FAIL — `GetActionHistory` method not found, `ListActionsByTarget`, `ListActionsByModerator`, `PenaltyLister`, `ListPenaltiesByActionID` not defined.

**Step 3: Expand the ModerationActionRepository interface and add PenaltyLister interface**

In `internal/service/moderation_action.go`, replace the `ModerationActionRepository` interface and add `PenaltyLister`:

```go
// ModerationActionRepository abstracts moderation action persistence.
type ModerationActionRepository interface {
	CreateModerationAction(ctx context.Context, action *domain.ModerationAction) error
	ListActionsByTarget(ctx context.Context, targetUserID string, limit, offset int) ([]*domain.ModerationAction, error)
	ListActionsByModerator(ctx context.Context, moderatorID string, limit, offset int) ([]*domain.ModerationAction, error)
}

// PenaltyLister extends PenaltyRepository with read operations.
type PenaltyLister interface {
	PenaltyRepository
	ListPenaltiesByActionID(ctx context.Context, actionID string) ([]domain.TrustPenalty, error)
}
```

Also update `ModerationActionService` to hold a `PenaltyLister`:

```go
type ModerationActionService struct {
	actions    ModerationActionRepository
	users      ActionUserLookup
	moderation *ModerationService
	enforcer   UserEnforcer
	penalties  PenaltyLister
	now        func() time.Time
}
```

Update `NewModerationActionService` to accept and store `PenaltyLister`:

```go
func NewModerationActionService(
	actions ModerationActionRepository,
	users ActionUserLookup,
	moderation *ModerationService,
	enforcer UserEnforcer,
	penalties PenaltyLister,
	clock func() time.Time,
) *ModerationActionService {
	if clock == nil {
		clock = time.Now
	}
	return &ModerationActionService{
		actions:    actions,
		users:      users,
		moderation: moderation,
		enforcer:   enforcer,
		penalties:  penalties,
		now:        clock,
	}
}
```

Add the `GetActionHistory` method and result type:

```go
// ActionHistoryEntry pairs a moderation action with its trust penalties.
type ActionHistoryEntry struct {
	Action    *domain.ModerationAction `json:"action"`
	Penalties []domain.TrustPenalty    `json:"penalties"`
}

// GetActionHistory returns moderation actions with their associated penalties.
// If byModerator is true, it lists actions taken BY the user (for council audit).
// Otherwise, it lists actions taken AGAINST the user.
func (s *ModerationActionService) GetActionHistory(
	ctx context.Context,
	userID string,
	byModerator bool,
	limit, offset int,
) ([]ActionHistoryEntry, error) {
	var actions []*domain.ModerationAction
	var err error

	if byModerator {
		actions, err = s.actions.ListActionsByModerator(ctx, userID, limit, offset)
	} else {
		actions, err = s.actions.ListActionsByTarget(ctx, userID, limit, offset)
	}
	if err != nil {
		return nil, fmt.Errorf("listing moderation actions: %w", err)
	}

	entries := make([]ActionHistoryEntry, 0, len(actions))
	for _, action := range actions {
		penalties, err := s.penalties.ListPenaltiesByActionID(ctx, action.ID)
		if err != nil {
			return nil, fmt.Errorf("listing penalties for action %s: %w", action.ID, err)
		}
		if penalties == nil {
			penalties = []domain.TrustPenalty{}
		}
		entries = append(entries, ActionHistoryEntry{
			Action:    action,
			Penalties: penalties,
		})
	}

	return entries, nil
}
```

**Step 4: Update all existing callers of NewModerationActionService**

Since we added a `penalties PenaltyLister` parameter, update call sites:

- In `internal/service/moderation_action_test.go`, update `newTestModerationActionService` to pass a `PenaltyLister` (the mock penalty repo can be extended to implement `PenaltyLister`, or pass nil if not needed for existing tests — use a combined mock).
- In `internal/handler/moderation_test.go`, update `newTestModerationActionService` similarly.
- In `cmd/bell/main.go` or wherever the real service is wired up (if it exists).

Note: The existing `mockActionRepo` in both test files only implements `CreateModerationAction`. Update the mocks to also implement the two new listing methods (returning empty slices by default).

**Step 5: Run tests to verify they pass**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/service/ -v`
Expected: All tests pass, including the new `GetActionHistory` tests.

**Step 6: Commit**

```bash
git add internal/service/moderation_action.go internal/service/moderation_action_history_test.go internal/service/moderation_action_test.go
git commit -m "feat: add GetActionHistory service method with penalty lookup"
```

---

### Task 3: Add handler for GET /api/v1/moderation/actions/{user_id}

**Files:**
- Modify: `internal/handler/moderation.go` (add ListActions method)
- Modify: `internal/handler/moderation_test.go` (add tests)

**Step 1: Write the failing tests**

Append to `internal/handler/moderation_test.go`:

```go
// --- ListActions: success (by target) ---

func TestModerationHandler_ListActions_ByTarget(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	actionRepo := newMockActionRepoH()
	// Seed actions that the mock will return
	actionRepo.actionsByTarget = []*domain.ModerationAction{
		{ID: "act-1", TargetUserID: "user-1", ModeratorID: "mod-1", Action: domain.ActionWarn, Severity: 1, Reason: "test", CreatedAt: now},
	}
	penaltyLister := newMockPenaltyListerH()
	penaltyLister.penalties["act-1"] = []domain.TrustPenalty{
		{ID: "pen-1", UserID: "user-1", ModerationActionID: "act-1", PenaltyAmount: 5.0, HopDepth: 0, CreatedAt: now},
	}

	svc := newTestModerationActionServiceWithPenalties(actionRepo, newMockActionUserLookup(), newMockPenaltyRepoH(), newMockPenaltyGraphH(), penaltyLister)
	h := handler.NewModerationHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/moderation/actions/user-1", nil)
	req = withUser(req, testModerator())
	req = withChiURLParam(req, "user_id", "user-1")
	rec := httptest.NewRecorder()

	h.ListActions(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Actions []service.ActionHistoryEntry `json:"actions"`
	}
	decodeBody(t, rec, &resp)
	if len(resp.Actions) != 1 {
		t.Fatalf("got %d actions, want 1", len(resp.Actions))
	}
	if resp.Actions[0].Action.ID != "act-1" {
		t.Errorf("action ID = %q, want %q", resp.Actions[0].Action.ID, "act-1")
	}
	if len(resp.Actions[0].Penalties) != 1 {
		t.Errorf("penalties = %d, want 1", len(resp.Actions[0].Penalties))
	}
}

// --- ListActions: no user in context ---

func TestModerationHandler_ListActions_NoUser(t *testing.T) {
	svc := newTestModerationActionService(
		newMockActionRepoH(), newMockActionUserLookup(),
		newMockPenaltyRepoH(), newMockPenaltyGraphH(),
	)
	h := handler.NewModerationHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/moderation/actions/user-1", nil)
	req = withChiURLParam(req, "user_id", "user-1")
	rec := httptest.NewRecorder()

	h.ListActions(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// --- ListActions: by moderator (council only) ---

func TestModerationHandler_ListActions_ByModerator_Council(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	actionRepo := newMockActionRepoH()
	actionRepo.actionsByModerator = []*domain.ModerationAction{
		{ID: "act-1", TargetUserID: "user-1", ModeratorID: "mod-2", Action: domain.ActionBan, Severity: 5, Reason: "banned", CreatedAt: now},
	}
	penaltyLister := newMockPenaltyListerH()

	svc := newTestModerationActionServiceWithPenalties(actionRepo, newMockActionUserLookup(), newMockPenaltyRepoH(), newMockPenaltyGraphH(), penaltyLister)
	h := handler.NewModerationHandler(svc)

	council := &domain.User{ID: "council-1", Role: domain.RoleCouncil, IsActive: true}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/moderation/actions/mod-2?role=moderator", nil)
	req = withUser(req, council)
	req = withChiURLParam(req, "user_id", "mod-2")
	rec := httptest.NewRecorder()

	h.ListActions(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

// --- ListActions: by moderator (non-council forbidden) ---

func TestModerationHandler_ListActions_ByModerator_NonCouncilForbidden(t *testing.T) {
	svc := newTestModerationActionService(
		newMockActionRepoH(), newMockActionUserLookup(),
		newMockPenaltyRepoH(), newMockPenaltyGraphH(),
	)
	h := handler.NewModerationHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/moderation/actions/mod-2?role=moderator", nil)
	req = withUser(req, testModerator())
	req = withChiURLParam(req, "user_id", "mod-2")
	rec := httptest.NewRecorder()

	h.ListActions(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

// --- ListActions: with pagination ---

func TestModerationHandler_ListActions_Pagination(t *testing.T) {
	actionRepo := newMockActionRepoH()
	penaltyLister := newMockPenaltyListerH()

	svc := newTestModerationActionServiceWithPenalties(actionRepo, newMockActionUserLookup(), newMockPenaltyRepoH(), newMockPenaltyGraphH(), penaltyLister)
	h := handler.NewModerationHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/moderation/actions/user-1?limit=10&offset=5", nil)
	req = withUser(req, testModerator())
	req = withChiURLParam(req, "user_id", "user-1")
	rec := httptest.NewRecorder()

	h.ListActions(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}
```

Note: The test file will also need updated mocks (see Task 2 note about updating existing mocks).

**Step 2: Run tests to verify they fail**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/handler/ -run TestModerationHandler_ListActions -v`
Expected: FAIL — `ListActions` method not found.

**Step 3: Implement ListActions handler**

Add to `internal/handler/moderation.go`:

```go
type listActionsResponse struct {
	Actions []service.ActionHistoryEntry `json:"actions"`
}

// ListActions handles GET /api/v1/moderation/actions/{user_id}.
func (h *ModerationHandler) ListActions(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	targetUserID := chi.URLParam(r, "user_id")
	byModerator := r.URL.Query().Get("role") == "moderator"

	// Only council can view moderator action history.
	if byModerator && !user.IsCouncil() {
		Error(w, http.StatusForbidden, "council role required")
		return
	}

	limit := parseLimit(r.URL.Query().Get("limit"))
	offset := parseOffset(r.URL.Query().Get("offset"))

	entries, err := h.actions.GetActionHistory(r.Context(), targetUserID, byModerator, limit, offset)
	if err != nil {
		serviceError(w, err)
		return
	}

	if entries == nil {
		entries = []service.ActionHistoryEntry{}
	}

	JSON(w, http.StatusOK, listActionsResponse{Actions: entries})
}
```

Add the `chi` import to `internal/handler/moderation.go` imports.

**Step 4: Run tests to verify they pass**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/handler/ -v`
Expected: All tests pass.

**Step 5: Commit**

```bash
git add internal/handler/moderation.go internal/handler/moderation_test.go
git commit -m "feat: add ListActions handler for moderation audit trail"
```

---

### Task 4: Register route and wire up dependencies

**Files:**
- Modify: `internal/server/routes.go` (add GET route)
- Modify: `cmd/bell/main.go` (wire PenaltyLister if needed)

**Step 1: Add the route**

In `internal/server/routes.go`, inside the moderation actions block (around line 66), add:

```go
if s.moderationActionService != nil {
	mh := handler.NewModerationHandler(s.moderationActionService)
	r.Post("/actions", mh.TakeAction)
	r.Get("/actions/{user_id}", mh.ListActions)
}
```

**Step 2: Verify the build compiles**

Run: `cd /home/jeremy/services/the-bell && go build ./...`
Expected: No errors.

**Step 3: Run all tests**

Run: `cd /home/jeremy/services/the-bell && go test ./... -v`
Expected: All tests pass.

**Step 4: Commit**

```bash
git add internal/server/routes.go
git commit -m "feat: register GET /api/v1/moderation/actions/{user_id} route"
```

---

### Task 5: Update existing tests and mocks for interface changes

This task may overlap with Tasks 2-4 depending on order of execution. The key changes:

**Files:**
- Modify: `internal/service/moderation_action_test.go` (update mock to implement expanded interface)
- Modify: `internal/handler/moderation_test.go` (update mock and helpers)

**Step 1: Update mockActionRepo in service tests**

The existing `mockActionRepo` in `internal/service/moderation_action_test.go` must implement the new `ListActionsByTarget` and `ListActionsByModerator` methods:

```go
type mockActionRepo struct {
	actions            []*domain.ModerationAction
	actionsByTarget    []*domain.ModerationAction
	actionsByModerator []*domain.ModerationAction
	createErr          error
	listErr            error
}

func (m *mockActionRepo) ListActionsByTarget(_ context.Context, _ string, _, _ int) ([]*domain.ModerationAction, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.actionsByTarget, nil
}

func (m *mockActionRepo) ListActionsByModerator(_ context.Context, _ string, _, _ int) ([]*domain.ModerationAction, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.actionsByModerator, nil
}
```

**Step 2: Update handler test mocks**

Similarly update the handler test's `mockActionRepo` and add penalty listing mock support. Also update `newTestModerationActionService` to pass a `PenaltyLister` (can be nil for existing tests that don't use `GetActionHistory`).

**Step 3: Run all tests**

Run: `cd /home/jeremy/services/the-bell && go test ./... -v`
Expected: All tests pass.

**Step 4: Commit**

```bash
git add internal/service/moderation_action_test.go internal/handler/moderation_test.go
git commit -m "test: update mocks for expanded moderation repository interfaces"
```

---

## Edge Cases

1. **Empty history**: User with no moderation actions should return `{"actions": []}`, not null.
2. **Non-existent user_id**: We don't validate user existence — just return empty results (consistent with list endpoints returning empty, not 404).
3. **Council audit**: Only `RoleCouncil` can use `?role=moderator`. Regular moderators get 403.
4. **Pagination bounds**: `limit` clamped to 1-100 (via existing `parseLimit`), `offset` defaults to 0.
5. **Penalty fetch failure**: If penalties fail to load for one action, the whole request fails (no partial results for read-only audit).

## Test Strategy

- **Unit tests (service)**: 5 tests covering by-target, by-moderator, empty, repo-error, penalty-error.
- **Unit tests (handler)**: 5 tests covering success, no-auth, council-only, non-council-forbidden, pagination.
- **All existing tests**: Must still pass after interface expansion (mock updates).
- **Manual verification**: `go build ./...` after each task ensures no compilation errors.

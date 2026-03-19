# Mute/Suspend/Ban Enforcement Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** When a moderation action (mute/suspend/ban) is taken, immediately enforce it by updating the target user's state, and add middleware to reject write requests from affected users.

**Architecture:** Enforcement is a new step in `ModerationActionService.TakeAction()` that runs after action persistence and penalty propagation. It updates user state (trust score, is_active, role) via a new `UserEnforcer` interface. A new `RequireActive` middleware rejects requests from suspended users. A `CanPost()` guard in the post Create handler rejects muted users.

**Tech Stack:** Go, chi router, standard library testing, existing sqlc queries

**Beads task:** `the-bell-dpv.3`

---

## Edge Cases & Design Decisions

1. **Mute enforcement**: Sets `trust_score = min(current, 29.0)` (one below the 30.0 posting threshold). If user's trust is already below 30, no-op. The trust penalty system (severity 3 = -25 pts) records the penalty independently for trust recalculation.

2. **Suspend enforcement**: Sets `is_active = false`. The user's Kratos session remains valid but all authenticated write endpoints reject them via `RequireActive` middleware.

3. **Ban enforcement**: Sets `role = banned` and `trust_score = 0`. Banned users are rejected by both `RequireRole(Member)` (role rank 0 < 2) and the trust system.

4. **Enforcement failure**: Follows the existing partial-success pattern — if enforcement fails after the action is persisted, return the result with the error. The handler logs it and returns 201.

5. **Duration expiry**: Out of scope. Mute/suspend expiry recovery (restoring trust or reactivating) requires a background job or lazy check. The `moderation_actions.expires_at` column exists for future use.

6. **Existing test breakage**: `testUser()` in post handler tests has `TrustScore: 0` (Go zero value). After adding the `CanPost()` check in Create, this must be updated to `TrustScore: 50.0`.

7. **Warn actions**: No enforcement needed — warnings are informational with trust penalties only.

---

### Task 1: Domain — Extract trust thresholds as named constants

**Files:**
- Modify: `internal/domain/user.go:29-35`
- Test: `internal/domain/user_test.go` (existing tests, no changes needed)

**Step 1: Add constants and update CanPost/CanVouch**

In `internal/domain/user.go`, add constants before the methods:

```go
const (
	PostingThreshold  = 30.0
	VouchingThreshold = 60.0
)
```

Update `CanPost()` and `CanVouch()`:

```go
func (u *User) CanPost() bool {
	return u.IsActive && u.TrustScore >= PostingThreshold && u.Role != RolePending && u.Role != RoleBanned
}

func (u *User) CanVouch() bool {
	return u.IsActive && u.TrustScore >= VouchingThreshold && u.Role != RolePending && u.Role != RoleBanned
}
```

**Step 2: Run existing tests to verify no regression**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/domain/ -v -run "TestUser_Can"`
Expected: All 8 CanPost + 6 CanVouch tests PASS (constants equal the old hardcoded values)

**Step 3: Commit**

```bash
git add internal/domain/user.go
git commit -m "refactor: extract trust threshold constants in domain"
```

---

### Task 2: Service — Add UserEnforcer interface and enforcement logic

**Files:**
- Modify: `internal/service/moderation_action.go`
- Modify: `internal/service/moderation_action_test.go`

**Step 1: Write failing tests for enforcement**

Add the mock and new tests to `internal/service/moderation_action_test.go`.

First, add the mock enforcer (after the existing mock types):

```go
// --- mock UserEnforcer ---

type mockUserEnforcer struct {
	trustUpdates  map[string]float64
	deactivated   map[string]bool
	roleUpdates   map[string]domain.Role
	trustErr      error
	deactivateErr error
	roleErr       error
}

func newMockUserEnforcer() *mockUserEnforcer {
	return &mockUserEnforcer{
		trustUpdates: make(map[string]float64),
		deactivated:  make(map[string]bool),
		roleUpdates:  make(map[string]domain.Role),
	}
}

func (m *mockUserEnforcer) UpdateUserTrustScore(_ context.Context, id string, score float64) error {
	if m.trustErr != nil {
		return m.trustErr
	}
	m.trustUpdates[id] = score
	return nil
}

func (m *mockUserEnforcer) DeactivateUser(_ context.Context, id string) error {
	if m.deactivateErr != nil {
		return m.deactivateErr
	}
	m.deactivated[id] = true
	return nil
}

func (m *mockUserEnforcer) UpdateUserRole(_ context.Context, id string, role domain.Role) error {
	if m.roleErr != nil {
		return m.roleErr
	}
	m.roleUpdates[id] = role
	return nil
}
```

Update the helper to accept the enforcer:

```go
func newTestModerationActionService(
	actions ModerationActionRepository,
	users ActionUserLookup,
	enforcer UserEnforcer,
	penalties PenaltyRepository,
	graph PenaltyGraphQuerier,
) *ModerationActionService {
	modSvc := NewModerationService(penalties, graph, fixedClock)
	return NewModerationActionService(actions, users, enforcer, modSvc, fixedClock)
}
```

Update ALL existing calls to `newTestModerationActionService` to pass `newMockUserEnforcer()` as the third argument. For example:

```go
// Before:
svc := newTestModerationActionService(
    newMockActionRepo(), newMockActionUserLookup(),
    newMockPenaltyRepo(), newMockPenaltyGraph(),
)
// After:
svc := newTestModerationActionService(
    newMockActionRepo(), newMockActionUserLookup(),
    newMockUserEnforcer(),
    newMockPenaltyRepo(), newMockPenaltyGraph(),
)
```

There are 17 test functions to update. Each call to `newTestModerationActionService` needs the enforcer inserted as the 3rd argument.

Then add the new enforcement tests:

```go
// --- Enforcement: mute drops trust below posting threshold ---

func TestModerationActionService_TakeAction_MuteEnforcesLowTrust(t *testing.T) {
	actionRepo := newMockActionRepo()
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, TrustScore: 80.0, Role: domain.RoleMember}
	enforcer := newMockUserEnforcer()
	penaltyRepo := newMockPenaltyRepo()
	graph := newMockPenaltyGraph()

	svc := newTestModerationActionService(actionRepo, users, enforcer, penaltyRepo, graph)

	dur := int64Ptr(3600)
	_, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionMute, 3, "spamming", dur)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	score, ok := enforcer.trustUpdates["target-1"]
	if !ok {
		t.Fatal("expected trust score update for target user")
	}
	if score != domain.PostingThreshold-1 {
		t.Errorf("trust score = %v, want %v", score, domain.PostingThreshold-1)
	}
}

// --- Enforcement: mute no-op when trust already low ---

func TestModerationActionService_TakeAction_MuteAlreadyLowTrust(t *testing.T) {
	actionRepo := newMockActionRepo()
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, TrustScore: 20.0, Role: domain.RoleMember}
	enforcer := newMockUserEnforcer()

	svc := newTestModerationActionService(actionRepo, users, enforcer, newMockPenaltyRepo(), newMockPenaltyGraph())

	dur := int64Ptr(3600)
	_, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionMute, 3, "spamming", dur)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := enforcer.trustUpdates["target-1"]; ok {
		t.Error("expected no trust update when trust already below threshold")
	}
}

// --- Enforcement: suspend deactivates user ---

func TestModerationActionService_TakeAction_SuspendDeactivates(t *testing.T) {
	actionRepo := newMockActionRepo()
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, TrustScore: 50.0, Role: domain.RoleMember}
	enforcer := newMockUserEnforcer()

	svc := newTestModerationActionService(actionRepo, users, enforcer, newMockPenaltyRepo(), newMockPenaltyGraph())

	dur := int64Ptr(86400)
	_, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionSuspend, 4, "repeated violations", dur)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !enforcer.deactivated["target-1"] {
		t.Error("expected user to be deactivated after suspend")
	}
}

// --- Enforcement: ban sets role + trust ---

func TestModerationActionService_TakeAction_BanEnforces(t *testing.T) {
	actionRepo := newMockActionRepo()
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, TrustScore: 50.0, Role: domain.RoleMember}
	enforcer := newMockUserEnforcer()

	svc := newTestModerationActionService(actionRepo, users, enforcer, newMockPenaltyRepo(), newMockPenaltyGraph())

	_, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionBan, 5, "permanent ban", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	role, ok := enforcer.roleUpdates["target-1"]
	if !ok {
		t.Fatal("expected role update for target user")
	}
	if role != domain.RoleBanned {
		t.Errorf("role = %q, want %q", role, domain.RoleBanned)
	}

	score, ok := enforcer.trustUpdates["target-1"]
	if !ok {
		t.Fatal("expected trust score update for target user")
	}
	if score != 0 {
		t.Errorf("trust score = %v, want 0", score)
	}
}

// --- Enforcement: warn has no enforcement ---

func TestModerationActionService_TakeAction_WarnNoEnforcement(t *testing.T) {
	actionRepo := newMockActionRepo()
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, TrustScore: 50.0, Role: domain.RoleMember}
	enforcer := newMockUserEnforcer()

	svc := newTestModerationActionService(actionRepo, users, enforcer, newMockPenaltyRepo(), newMockPenaltyGraph())

	_, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionWarn, 1, "first warning", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(enforcer.trustUpdates) != 0 {
		t.Error("expected no trust updates for warn")
	}
	if len(enforcer.deactivated) != 0 {
		t.Error("expected no deactivation for warn")
	}
	if len(enforcer.roleUpdates) != 0 {
		t.Error("expected no role updates for warn")
	}
}

// --- Enforcement failure returns partial result ---

func TestModerationActionService_TakeAction_EnforcementFailure(t *testing.T) {
	actionRepo := newMockActionRepo()
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, TrustScore: 50.0, Role: domain.RoleMember}
	enforcer := newMockUserEnforcer()
	enforcer.deactivateErr = errors.New("db down")

	svc := newTestModerationActionService(actionRepo, users, enforcer, newMockPenaltyRepo(), newMockPenaltyGraph())

	dur := int64Ptr(86400)
	result, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionSuspend, 4, "suspend", dur)
	if err == nil {
		t.Fatal("expected error from enforcement failure")
	}
	if result == nil || result.Action == nil {
		t.Fatal("expected partial result with action despite enforcement failure")
	}
	if len(result.Penalties) == 0 {
		t.Error("expected penalties despite enforcement failure")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/service/ -v -run "TestModerationActionService" -count=1`
Expected: Compilation error — `UserEnforcer` type not defined, `NewModerationActionService` signature mismatch

**Step 3: Implement UserEnforcer interface and enforcement logic**

In `internal/service/moderation_action.go`:

Add the `UserEnforcer` interface after `ActionUserLookup`:

```go
// UserEnforcer applies immediate state changes to a user for moderation enforcement.
type UserEnforcer interface {
	UpdateUserTrustScore(ctx context.Context, id string, score float64) error
	DeactivateUser(ctx context.Context, id string) error
	UpdateUserRole(ctx context.Context, id string, role domain.Role) error
}
```

Update the struct to include the enforcer:

```go
type ModerationActionService struct {
	actions    ModerationActionRepository
	users      ActionUserLookup
	enforcer   UserEnforcer
	moderation *ModerationService
	now        func() time.Time
}
```

Update the constructor:

```go
func NewModerationActionService(
	actions ModerationActionRepository,
	users ActionUserLookup,
	enforcer UserEnforcer,
	moderation *ModerationService,
	clock func() time.Time,
) *ModerationActionService {
	if clock == nil {
		clock = time.Now
	}
	return &ModerationActionService{
		actions:    actions,
		users:      users,
		enforcer:   enforcer,
		moderation: moderation,
		now:        clock,
	}
}
```

In `TakeAction`, change the discarded user lookup to capture the result:

```go
// Before:
if _, err := s.users.GetUserByID(ctx, targetUserID); err != nil {
    return nil, err
}

// After:
targetUser, err := s.users.GetUserByID(ctx, targetUserID)
if err != nil {
    return nil, err
}
```

Add the enforcement call after penalty propagation (before the final return):

```go
	penalties, err := s.moderation.PropagatePenalties(ctx, action.ID, targetUserID, severity)
	if err != nil {
		return &TakeActionResult{Action: action, Penalties: penalties}, fmt.Errorf("propagating penalties: %w", err)
	}

	// Enforce the action on the target user's state
	if err := s.enforce(ctx, actionType, targetUser); err != nil {
		return &TakeActionResult{Action: action, Penalties: penalties}, fmt.Errorf("enforcing action: %w", err)
	}

	return &TakeActionResult{Action: action, Penalties: penalties}, nil
```

Add the `enforce` method:

```go
// enforce applies immediate user state changes for the given action type.
func (s *ModerationActionService) enforce(ctx context.Context, actionType domain.ActionType, targetUser *domain.User) error {
	switch actionType {
	case domain.ActionMute:
		if targetUser.TrustScore >= domain.PostingThreshold {
			return s.enforcer.UpdateUserTrustScore(ctx, targetUser.ID, domain.PostingThreshold-1)
		}
	case domain.ActionSuspend:
		return s.enforcer.DeactivateUser(ctx, targetUser.ID)
	case domain.ActionBan:
		if err := s.enforcer.UpdateUserRole(ctx, targetUser.ID, domain.RoleBanned); err != nil {
			return err
		}
		return s.enforcer.UpdateUserTrustScore(ctx, targetUser.ID, 0)
	}
	return nil
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/service/ -v -run "TestModerationActionService" -count=1`
Expected: ALL tests PASS (existing + new)

**Step 5: Commit**

```bash
git add internal/service/moderation_action.go internal/service/moderation_action_test.go
git commit -m "feat: enforce mute/suspend/ban on user state in TakeAction"
```

---

### Task 3: Middleware — RequireActive

**Files:**
- Modify: `internal/middleware/auth.go`
- Modify: `internal/middleware/auth_test.go`

**Step 1: Write failing tests for RequireActive**

Add to `internal/middleware/auth_test.go`:

```go
// --- RequireActive tests ---

func TestRequireActive_NoUserInContext(t *testing.T) {
	handler := middleware.RequireActive(okHandler())

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusUnauthorized)
	assertErrorBody(t, rec, "unauthorized")
}

func TestRequireActive_InactiveUser(t *testing.T) {
	user := &domain.User{ID: "u1", Role: domain.RoleMember, IsActive: false}
	handler := middleware.RequireActive(okHandler())

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	ctx := middleware.WithUser(req.Context(), user)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusForbidden)
	assertErrorBody(t, rec, "account suspended")
}

func TestRequireActive_ActiveUser(t *testing.T) {
	user := &domain.User{ID: "u1", Role: domain.RoleMember, IsActive: true}
	handler := middleware.RequireActive(okHandler())

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	ctx := middleware.WithUser(req.Context(), user)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/middleware/ -v -run "TestRequireActive" -count=1`
Expected: Compilation error — `RequireActive` not defined

**Step 3: Implement RequireActive**

Add to `internal/middleware/auth.go`, after the `RequireRole` function:

```go
// RequireActive rejects requests from users whose accounts are not active
// (i.e., suspended users).
func RequireActive(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		if !user.IsActive {
			writeError(w, http.StatusForbidden, "account suspended")
			return
		}

		next.ServeHTTP(w, r)
	})
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/middleware/ -v -count=1`
Expected: ALL middleware tests PASS

**Step 5: Commit**

```bash
git add internal/middleware/auth.go internal/middleware/auth_test.go
git commit -m "feat: add RequireActive middleware to block suspended users"
```

---

### Task 4: Handler — CanPost guard in post Create

**Files:**
- Modify: `internal/handler/post.go:44-64`
- Modify: `internal/handler/post_test.go`

**Step 1: Fix testUser trust score and write failing tests**

In `internal/handler/post_test.go`, update `testUser()` to have a valid trust score:

```go
func testUser() *domain.User {
	return &domain.User{
		ID:         "user-1",
		Role:       domain.RoleMember,
		IsActive:   true,
		TrustScore: 50.0,
	}
}
```

Then add the new test:

```go
func TestPostHandler_Create_CannotPost(t *testing.T) {
	tests := []struct {
		name string
		user *domain.User
	}{
		{"muted (low trust)", &domain.User{ID: "u1", Role: domain.RoleMember, IsActive: true, TrustScore: 20.0}},
		{"suspended", &domain.User{ID: "u1", Role: domain.RoleMember, IsActive: false, TrustScore: 50.0}},
		{"banned", &domain.User{ID: "u1", Role: domain.RoleBanned, IsActive: true, TrustScore: 50.0}},
		{"pending", &domain.User{ID: "u1", Role: domain.RolePending, IsActive: true, TrustScore: 50.0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockPostRepo()
			svc := newTestPostService(repo)
			h := handler.NewPostHandler(svc)

			body := `{"body":"Hello, world!"}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", strings.NewReader(body))
			req = withUser(req, tt.user)
			rec := httptest.NewRecorder()

			h.Create(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
			}
		})
	}
}
```

**Step 2: Run tests to verify the new test fails**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/handler/ -v -run "TestPostHandler_Create_CannotPost" -count=1`
Expected: FAIL — handler returns 201 instead of 403

**Step 3: Add CanPost check to Create handler**

In `internal/handler/post.go`, add the `CanPost()` check after the user extraction:

```go
func (h *PostHandler) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if !user.CanPost() {
		Error(w, http.StatusForbidden, "posting not allowed")
		return
	}

	var req createPostRequest
	// ... rest unchanged
```

**Step 4: Run ALL handler tests**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/handler/ -v -count=1`
Expected: ALL tests PASS (existing tests pass because testUser() now has TrustScore: 50.0)

**Step 5: Commit**

```bash
git add internal/handler/post.go internal/handler/post_test.go
git commit -m "feat: add CanPost guard to post creation handler"
```

---

### Task 5: Routes — Wire RequireActive into authenticated groups

**Files:**
- Modify: `internal/server/routes.go`
- Test: `internal/server/server_test.go` (existing)

**Step 1: Add RequireActive to all authenticated route groups**

In `internal/server/routes.go`, add `middleware.RequireActive` after each `s.authMiddleware` usage:

```go
func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.ContentTypeJSON)
	r.Use(middleware.RequestLogger(s.logger))
	r.Get("/healthz", handler.Health)

	if s.postService != nil {
		ph := handler.NewPostHandler(s.postService)
		r.Route("/api/v1/posts", func(r chi.Router) {
			r.Get("/", ph.ListFeed)
			r.Get("/{id}", ph.GetByID)

			r.Group(func(r chi.Router) {
				if s.authMiddleware != nil {
					r.Use(s.authMiddleware)
				}
				r.Use(middleware.RequireActive)
				r.Use(middleware.RequireRole(domain.RoleMember))
				r.Post("/", ph.Create)
				r.Patch("/{id}", ph.Update)
				r.Delete("/{id}", ph.Delete)
			})
		})
	}

	if s.reportService != nil {
		rh := handler.NewReportHandler(s.reportService)

		r.Route("/api/v1/posts/{id}/report", func(r chi.Router) {
			if s.authMiddleware != nil {
				r.Use(s.authMiddleware)
			}
			r.Use(middleware.RequireActive)
			r.Use(middleware.RequireRole(domain.RoleMember))
			r.Post("/", rh.SubmitReport)
		})
	}

	if s.reportService != nil || s.moderationActionService != nil {
		r.Route("/api/v1/moderation", func(r chi.Router) {
			if s.authMiddleware != nil {
				r.Use(s.authMiddleware)
			}
			r.Use(middleware.RequireActive)
			r.Use(middleware.RequireRole(domain.RoleModerator))

			if s.reportService != nil {
				rh := handler.NewReportHandler(s.reportService)
				r.Get("/queue", rh.ListQueue)
			}

			if s.moderationActionService != nil {
				mh := handler.NewModerationHandler(s.moderationActionService)
				r.Post("/actions", mh.TakeAction)
			}
		})
	}

	return r
}
```

**Step 2: Run all tests**

Run: `cd /home/jeremy/services/the-bell && go test ./... -count=1`
Expected: ALL tests PASS

**Step 3: Commit**

```bash
git add internal/server/routes.go
git commit -m "feat: wire RequireActive middleware into all authenticated routes"
```

---

### Task 6: Final verification

**Step 1: Run full test suite**

Run: `cd /home/jeremy/services/the-bell && go test ./... -v -count=1`
Expected: ALL tests PASS

**Step 2: Run go vet**

Run: `cd /home/jeremy/services/the-bell && go vet ./...`
Expected: No issues

**Step 3: Verify compilation**

Run: `cd /home/jeremy/services/the-bell && go build ./...`
Expected: Clean build

**Step 4: Close the beads task**

```bash
bd close the-bell-dpv.3 --reason="Implemented mute/suspend/ban enforcement: mute drops trust below posting threshold, suspend deactivates user, ban sets role=banned with trust=0. Added RequireActive middleware and CanPost guard in post handler."
```

**Step 5: Push**

```bash
git push
```

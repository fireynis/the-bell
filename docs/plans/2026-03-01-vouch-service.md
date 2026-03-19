# Vouch Service Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create a VouchService that enforces trust-based vouching rules (trust>=60, daily limit 3, cycle detection), manages vouch graph edges, and auto-promotes pending users to member when vouched.

**Architecture:** Single service file at `internal/service/vouch.go` following the existing PostService/UserService pattern: defines its own repository interfaces using domain types, uses clock injection for testability, generates UUIDv7 IDs. Tests use in-memory mock repositories in the same package. The service depends on three interfaces: `VouchRepository` (CRUD), `GraphQuerier` (AGE Cypher ops), and `UserGetter` (user lookup + role update).

**Tech Stack:** Go stdlib `testing`, `github.com/google/uuid` (UUIDv7), `github.com/fireynis/the-bell/internal/domain`

**Beads Task:** `the-bell-8ee.2`

---

### Task 1: VouchService scaffold with Vouch method — failing tests

**Files:**
- Create: `internal/service/vouch.go`
- Create: `internal/service/vouch_test.go`

**Step 1: Write the failing tests**

Create `internal/service/vouch_test.go` with mock implementations and the first batch of tests:

```go
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// --- Mock VouchRepository ---

type mockVouchRepo struct {
	vouches  map[string]*domain.Vouch // keyed by ID
	byPair   map[string]*domain.Vouch // keyed by "voucherID|voucheeID"
	counts   map[string]int64         // keyed by voucherID — count of vouches since cutoff

	createErr error
	revokeErr error
}

func newMockVouchRepo() *mockVouchRepo {
	return &mockVouchRepo{
		vouches: make(map[string]*domain.Vouch),
		byPair:  make(map[string]*domain.Vouch),
		counts:  make(map[string]int64),
	}
}

func pairKey(voucherID, voucheeID string) string {
	return voucherID + "|" + voucheeID
}

func (m *mockVouchRepo) CreateVouch(_ context.Context, v *domain.Vouch) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.vouches[v.ID] = v
	m.byPair[pairKey(v.VoucherID, v.VoucheeID)] = v
	return nil
}

func (m *mockVouchRepo) GetVouchByID(_ context.Context, id string) (*domain.Vouch, error) {
	v, ok := m.vouches[id]
	if !ok {
		return nil, ErrNotFound
	}
	return v, nil
}

func (m *mockVouchRepo) GetVouchByPair(_ context.Context, voucherID, voucheeID string) (*domain.Vouch, error) {
	v, ok := m.byPair[pairKey(voucherID, voucheeID)]
	if !ok {
		return nil, ErrNotFound
	}
	return v, nil
}

func (m *mockVouchRepo) RevokeVouch(_ context.Context, id string) error {
	if m.revokeErr != nil {
		return m.revokeErr
	}
	v, ok := m.vouches[id]
	if !ok {
		return ErrNotFound
	}
	v.Status = domain.VouchRevoked
	now := time.Now()
	v.RevokedAt = &now
	return nil
}

func (m *mockVouchRepo) CountVouchesByVoucherSince(_ context.Context, voucherID string, _ time.Time) (int64, error) {
	return m.counts[voucherID], nil
}

// --- Mock GraphQuerier ---

type mockGraph struct {
	edges    map[string]bool // "voucherID|voucheeID" → exists
	cycleFor map[string]bool // "voucherID|voucheeID" → would create cycle

	addErr    error
	removeErr error
	cycleErr  error
}

func newMockGraph() *mockGraph {
	return &mockGraph{
		edges:    make(map[string]bool),
		cycleFor: make(map[string]bool),
	}
}

func (m *mockGraph) AddVouchEdge(_ context.Context, voucherID, voucheeID string) error {
	if m.addErr != nil {
		return m.addErr
	}
	m.edges[pairKey(voucherID, voucheeID)] = true
	return nil
}

func (m *mockGraph) RemoveVouchEdge(_ context.Context, voucherID, voucheeID string) error {
	if m.removeErr != nil {
		return m.removeErr
	}
	delete(m.edges, pairKey(voucherID, voucheeID))
	return nil
}

func (m *mockGraph) HasCyclicVouch(_ context.Context, voucherID, voucheeID string) (bool, error) {
	if m.cycleErr != nil {
		return false, m.cycleErr
	}
	return m.cycleFor[pairKey(voucherID, voucheeID)], nil
}

// --- Mock UserGetter ---

type mockUserGetter struct {
	users       map[string]*domain.User
	roleUpdates map[string]domain.Role // tracks UpdateUserRole calls

	getErr  error
	roleErr error
}

func newMockUserGetter() *mockUserGetter {
	return &mockUserGetter{
		users:       make(map[string]*domain.User),
		roleUpdates: make(map[string]domain.Role),
	}
}

func (m *mockUserGetter) GetUserByID(_ context.Context, id string) (*domain.User, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	u, ok := m.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	return u, nil
}

func (m *mockUserGetter) UpdateUserRole(_ context.Context, id string, role domain.Role) error {
	if m.roleErr != nil {
		return m.roleErr
	}
	u, ok := m.users[id]
	if !ok {
		return ErrNotFound
	}
	u.Role = role
	m.roleUpdates[id] = role
	return nil
}

// --- Helper ---

func makeUser(id string, role domain.Role, trust float64) *domain.User {
	return &domain.User{
		ID:         id,
		Role:       role,
		TrustScore: trust,
		IsActive:   true,
	}
}

func newTestVouchService(repo *mockVouchRepo, graph *mockGraph, users *mockUserGetter, now time.Time) *VouchService {
	return NewVouchService(repo, graph, users, func() time.Time { return now })
}

// --- Tests ---

func TestVouchService_Vouch_Success(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()

	users.users["voucher-1"] = makeUser("voucher-1", domain.RoleMember, 75.0)
	users.users["vouchee-1"] = makeUser("vouchee-1", domain.RolePending, 50.0)

	svc := newTestVouchService(repo, graph, users, now)

	vouch, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if err != nil {
		t.Fatalf("Vouch() unexpected error: %v", err)
	}
	if vouch.ID == "" {
		t.Error("Vouch() returned empty ID")
	}
	if vouch.VoucherID != "voucher-1" {
		t.Errorf("VoucherID = %q, want %q", vouch.VoucherID, "voucher-1")
	}
	if vouch.VoucheeID != "vouchee-1" {
		t.Errorf("VoucheeID = %q, want %q", vouch.VoucheeID, "vouchee-1")
	}
	if vouch.Status != domain.VouchActive {
		t.Errorf("Status = %q, want %q", vouch.Status, domain.VouchActive)
	}
	if !vouch.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", vouch.CreatedAt, now)
	}

	// Verify vouch stored in repo
	if _, ok := repo.vouches[vouch.ID]; !ok {
		t.Error("vouch not stored in repository")
	}

	// Verify graph edge created
	if !graph.edges[pairKey("voucher-1", "vouchee-1")] {
		t.Error("graph edge not created")
	}
}

func TestVouchService_Vouch_PromotesPendingToMember(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()

	users.users["voucher-1"] = makeUser("voucher-1", domain.RoleMember, 75.0)
	users.users["vouchee-1"] = makeUser("vouchee-1", domain.RolePending, 50.0)

	svc := newTestVouchService(repo, graph, users, now)

	_, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if err != nil {
		t.Fatalf("Vouch() unexpected error: %v", err)
	}

	if users.users["vouchee-1"].Role != domain.RoleMember {
		t.Errorf("vouchee role = %q, want %q (should be promoted)", users.users["vouchee-1"].Role, domain.RoleMember)
	}
	if users.roleUpdates["vouchee-1"] != domain.RoleMember {
		t.Error("UpdateUserRole not called for vouchee promotion")
	}
}

func TestVouchService_Vouch_NoPromoteIfAlreadyMember(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()

	users.users["voucher-1"] = makeUser("voucher-1", domain.RoleMember, 75.0)
	users.users["vouchee-1"] = makeUser("vouchee-1", domain.RoleMember, 65.0)

	svc := newTestVouchService(repo, graph, users, now)

	_, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if err != nil {
		t.Fatalf("Vouch() unexpected error: %v", err)
	}

	if _, called := users.roleUpdates["vouchee-1"]; called {
		t.Error("UpdateUserRole should not be called when vouchee is already a member")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/service/ -v -run TestVouchService_Vouch`
Expected: FAIL — `NewVouchService`, `VouchService`, and `Vouch` don't exist yet.

**Step 3: Write minimal implementation**

Create `internal/service/vouch.go`:

```go
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/google/uuid"
)

const maxDailyVouches = 3

// VouchRepository abstracts vouch persistence using domain types.
type VouchRepository interface {
	CreateVouch(ctx context.Context, vouch *domain.Vouch) error
	GetVouchByID(ctx context.Context, id string) (*domain.Vouch, error)
	GetVouchByPair(ctx context.Context, voucherID, voucheeID string) (*domain.Vouch, error)
	RevokeVouch(ctx context.Context, id string) error
	CountVouchesByVoucherSince(ctx context.Context, voucherID string, since time.Time) (int64, error)
}

// GraphQuerier abstracts Apache AGE graph operations for the vouch trust graph.
type GraphQuerier interface {
	AddVouchEdge(ctx context.Context, voucherID, voucheeID string) error
	RemoveVouchEdge(ctx context.Context, voucherID, voucheeID string) error
	HasCyclicVouch(ctx context.Context, voucherID, voucheeID string) (bool, error)
}

// UserGetter abstracts user lookup and role updates needed by VouchService.
type UserGetter interface {
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
	UpdateUserRole(ctx context.Context, id string, role domain.Role) error
}

// VouchService orchestrates vouch business logic.
type VouchService struct {
	repo  VouchRepository
	graph GraphQuerier
	users UserGetter
	now   func() time.Time
}

func NewVouchService(repo VouchRepository, graph GraphQuerier, users UserGetter, clock func() time.Time) *VouchService {
	if clock == nil {
		clock = time.Now
	}
	return &VouchService{
		repo:  repo,
		graph: graph,
		users: users,
		now:   clock,
	}
}

// Vouch creates a trust vouch from voucherID to voucheeID. It enforces:
//   - Voucher must have trust >= 60 (CanVouch)
//   - No self-vouching
//   - No duplicate active vouches for the same pair
//   - Daily limit of 3 vouches per voucher
//   - No cycles in the trust graph
//
// On success, it creates the vouch record and graph edge. If the vouchee
// is pending, they are promoted to member.
func (s *VouchService) Vouch(ctx context.Context, voucherID, voucheeID string) (*domain.Vouch, error) {
	if voucherID == voucheeID {
		return nil, fmt.Errorf("%w: cannot vouch for yourself", ErrValidation)
	}

	voucher, err := s.users.GetUserByID(ctx, voucherID)
	if err != nil {
		return nil, fmt.Errorf("looking up voucher: %w", err)
	}
	if !voucher.CanVouch() {
		return nil, fmt.Errorf("%w: voucher does not meet trust requirements", ErrForbidden)
	}

	vouchee, err := s.users.GetUserByID(ctx, voucheeID)
	if err != nil {
		return nil, fmt.Errorf("looking up vouchee: %w", err)
	}

	// Check for existing vouch (any status — unique constraint prevents re-creation)
	_, err = s.repo.GetVouchByPair(ctx, voucherID, voucheeID)
	if err == nil {
		return nil, fmt.Errorf("%w: vouch already exists for this pair", ErrValidation)
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("checking existing vouch: %w", err)
	}

	// Daily rate limit
	cutoff := s.now().AddDate(0, 0, -1)
	count, err := s.repo.CountVouchesByVoucherSince(ctx, voucherID, cutoff)
	if err != nil {
		return nil, fmt.Errorf("checking daily vouch count: %w", err)
	}
	if count >= maxDailyVouches {
		return nil, fmt.Errorf("%w: daily vouch limit of %d reached", ErrValidation, maxDailyVouches)
	}

	// Cycle detection
	hasCycle, err := s.graph.HasCyclicVouch(ctx, voucherID, voucheeID)
	if err != nil {
		return nil, fmt.Errorf("checking vouch cycle: %w", err)
	}
	if hasCycle {
		return nil, fmt.Errorf("%w: vouch would create a cycle in the trust graph", ErrValidation)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generating vouch id: %w", err)
	}

	vouch := &domain.Vouch{
		ID:        id.String(),
		VoucherID: voucherID,
		VoucheeID: voucheeID,
		Status:    domain.VouchActive,
		CreatedAt: s.now(),
	}

	if err := s.repo.CreateVouch(ctx, vouch); err != nil {
		return nil, fmt.Errorf("creating vouch: %w", err)
	}

	if err := s.graph.AddVouchEdge(ctx, voucherID, voucheeID); err != nil {
		return nil, fmt.Errorf("adding vouch graph edge: %w", err)
	}

	// Promote pending vouchee to member
	if vouchee.Role == domain.RolePending {
		if err := s.users.UpdateUserRole(ctx, voucheeID, domain.RoleMember); err != nil {
			return nil, fmt.Errorf("promoting vouchee to member: %w", err)
		}
	}

	return vouch, nil
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/service/ -v -run TestVouchService_Vouch`
Expected: PASS — all 3 Vouch tests pass.

**Step 5: Commit**

```bash
cd /home/jeremy/services/the-bell
git add internal/service/vouch.go internal/service/vouch_test.go
git commit -m "feat: add VouchService with Vouch method (trust check, rate limit, cycle detection, promotion)"
```

---

### Task 2: Vouch validation error tests

**Files:**
- Modify: `internal/service/vouch_test.go`

**Step 1: Write the failing tests**

Append to `internal/service/vouch_test.go`:

```go
func TestVouchService_Vouch_SelfVouch(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()

	users.users["user-1"] = makeUser("user-1", domain.RoleMember, 75.0)

	svc := newTestVouchService(repo, graph, users, now)

	_, err := svc.Vouch(context.Background(), "user-1", "user-1")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Vouch() error = %v, want %v", err, ErrValidation)
	}
}

func TestVouchService_Vouch_LowTrust(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()

	users.users["voucher-1"] = makeUser("voucher-1", domain.RoleMember, 55.0) // below 60
	users.users["vouchee-1"] = makeUser("vouchee-1", domain.RolePending, 50.0)

	svc := newTestVouchService(repo, graph, users, now)

	_, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Vouch() error = %v, want %v", err, ErrForbidden)
	}
}

func TestVouchService_Vouch_PendingVoucherForbidden(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()

	users.users["voucher-1"] = makeUser("voucher-1", domain.RolePending, 75.0) // pending can't vouch
	users.users["vouchee-1"] = makeUser("vouchee-1", domain.RolePending, 50.0)

	svc := newTestVouchService(repo, graph, users, now)

	_, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Vouch() error = %v, want %v", err, ErrForbidden)
	}
}

func TestVouchService_Vouch_BannedVoucherForbidden(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()

	users.users["voucher-1"] = makeUser("voucher-1", domain.RoleBanned, 75.0)
	users.users["vouchee-1"] = makeUser("vouchee-1", domain.RolePending, 50.0)

	svc := newTestVouchService(repo, graph, users, now)

	_, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Vouch() error = %v, want %v", err, ErrForbidden)
	}
}

func TestVouchService_Vouch_DuplicateVouch(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()

	users.users["voucher-1"] = makeUser("voucher-1", domain.RoleMember, 75.0)
	users.users["vouchee-1"] = makeUser("vouchee-1", domain.RolePending, 50.0)

	// Pre-existing vouch
	repo.byPair[pairKey("voucher-1", "vouchee-1")] = &domain.Vouch{
		ID:        "existing-vouch",
		VoucherID: "voucher-1",
		VoucheeID: "vouchee-1",
		Status:    domain.VouchActive,
	}

	svc := newTestVouchService(repo, graph, users, now)

	_, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Vouch() error = %v, want %v", err, ErrValidation)
	}
}

func TestVouchService_Vouch_DailyLimitReached(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()

	users.users["voucher-1"] = makeUser("voucher-1", domain.RoleMember, 75.0)
	users.users["vouchee-1"] = makeUser("vouchee-1", domain.RolePending, 50.0)

	repo.counts["voucher-1"] = 3 // already at limit

	svc := newTestVouchService(repo, graph, users, now)

	_, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Vouch() error = %v, want %v", err, ErrValidation)
	}
}

func TestVouchService_Vouch_CycleDetected(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()

	users.users["voucher-1"] = makeUser("voucher-1", domain.RoleMember, 75.0)
	users.users["vouchee-1"] = makeUser("vouchee-1", domain.RoleMember, 65.0)

	graph.cycleFor[pairKey("voucher-1", "vouchee-1")] = true

	svc := newTestVouchService(repo, graph, users, now)

	_, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Vouch() error = %v, want %v", err, ErrValidation)
	}
}

func TestVouchService_Vouch_VoucherNotFound(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()

	svc := newTestVouchService(repo, graph, users, now)

	_, err := svc.Vouch(context.Background(), "no-such-user", "vouchee-1")
	if err == nil {
		t.Fatal("Vouch() expected error, got nil")
	}
}

func TestVouchService_Vouch_VoucheeNotFound(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()

	users.users["voucher-1"] = makeUser("voucher-1", domain.RoleMember, 75.0)

	svc := newTestVouchService(repo, graph, users, now)

	_, err := svc.Vouch(context.Background(), "voucher-1", "no-such-user")
	if err == nil {
		t.Fatal("Vouch() expected error, got nil")
	}
}
```

**Step 2: Run tests to verify they pass**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/service/ -v -run TestVouchService_Vouch`
Expected: PASS — all validation tests pass (implementation from Task 1 already handles these).

**Step 3: Commit**

```bash
cd /home/jeremy/services/the-bell
git add internal/service/vouch_test.go
git commit -m "test: add vouch validation error tests (self-vouch, trust, rate limit, cycle, duplicate)"
```

---

### Task 3: Revoke method — failing test, then implementation

**Files:**
- Modify: `internal/service/vouch.go`
- Modify: `internal/service/vouch_test.go`

**Step 1: Write the failing tests**

Append to `internal/service/vouch_test.go`:

```go
func TestVouchService_Revoke_ByVoucher(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()

	users.users["voucher-1"] = makeUser("voucher-1", domain.RoleMember, 75.0)

	existing := &domain.Vouch{
		ID:        "vouch-1",
		VoucherID: "voucher-1",
		VoucheeID: "vouchee-1",
		Status:    domain.VouchActive,
		CreatedAt: now.Add(-24 * time.Hour),
	}
	repo.vouches["vouch-1"] = existing

	graph.edges[pairKey("voucher-1", "vouchee-1")] = true

	svc := newTestVouchService(repo, graph, users, now)

	err := svc.Revoke(context.Background(), "vouch-1", "voucher-1")
	if err != nil {
		t.Fatalf("Revoke() unexpected error: %v", err)
	}

	// Verify vouch revoked
	if existing.Status != domain.VouchRevoked {
		t.Errorf("Status = %q, want %q", existing.Status, domain.VouchRevoked)
	}

	// Verify graph edge removed
	if graph.edges[pairKey("voucher-1", "vouchee-1")] {
		t.Error("graph edge should have been removed")
	}
}

func TestVouchService_Revoke_ByModerator(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()

	users.users["mod-1"] = makeUser("mod-1", domain.RoleModerator, 80.0)

	existing := &domain.Vouch{
		ID:        "vouch-1",
		VoucherID: "voucher-1",
		VoucheeID: "vouchee-1",
		Status:    domain.VouchActive,
		CreatedAt: now.Add(-24 * time.Hour),
	}
	repo.vouches["vouch-1"] = existing

	graph.edges[pairKey("voucher-1", "vouchee-1")] = true

	svc := newTestVouchService(repo, graph, users, now)

	err := svc.Revoke(context.Background(), "vouch-1", "mod-1")
	if err != nil {
		t.Fatalf("Revoke() unexpected error: %v", err)
	}

	if existing.Status != domain.VouchRevoked {
		t.Errorf("Status = %q, want %q", existing.Status, domain.VouchRevoked)
	}
}

func TestVouchService_Revoke_ByCouncil(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()

	users.users["council-1"] = makeUser("council-1", domain.RoleCouncil, 90.0)

	existing := &domain.Vouch{
		ID:        "vouch-1",
		VoucherID: "voucher-1",
		VoucheeID: "vouchee-1",
		Status:    domain.VouchActive,
		CreatedAt: now.Add(-24 * time.Hour),
	}
	repo.vouches["vouch-1"] = existing

	graph.edges[pairKey("voucher-1", "vouchee-1")] = true

	svc := newTestVouchService(repo, graph, users, now)

	err := svc.Revoke(context.Background(), "vouch-1", "council-1")
	if err != nil {
		t.Fatalf("Revoke() unexpected error: %v", err)
	}

	if existing.Status != domain.VouchRevoked {
		t.Errorf("Status = %q, want %q", existing.Status, domain.VouchRevoked)
	}
}

func TestVouchService_Revoke_UnauthorizedUser(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()

	users.users["other-user"] = makeUser("other-user", domain.RoleMember, 65.0)

	existing := &domain.Vouch{
		ID:        "vouch-1",
		VoucherID: "voucher-1",
		VoucheeID: "vouchee-1",
		Status:    domain.VouchActive,
		CreatedAt: now.Add(-24 * time.Hour),
	}
	repo.vouches["vouch-1"] = existing

	svc := newTestVouchService(repo, graph, users, now)

	err := svc.Revoke(context.Background(), "vouch-1", "other-user")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Revoke() error = %v, want %v", err, ErrForbidden)
	}
}

func TestVouchService_Revoke_NotFound(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()

	users.users["user-1"] = makeUser("user-1", domain.RoleMember, 75.0)

	svc := newTestVouchService(repo, graph, users, now)

	err := svc.Revoke(context.Background(), "nonexistent", "user-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Revoke() error = %v, want %v", err, ErrNotFound)
	}
}

func TestVouchService_Revoke_AlreadyRevoked(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()

	users.users["voucher-1"] = makeUser("voucher-1", domain.RoleMember, 75.0)

	revokedAt := now.Add(-1 * time.Hour)
	existing := &domain.Vouch{
		ID:        "vouch-1",
		VoucherID: "voucher-1",
		VoucheeID: "vouchee-1",
		Status:    domain.VouchRevoked,
		RevokedAt: &revokedAt,
		CreatedAt: now.Add(-24 * time.Hour),
	}
	repo.vouches["vouch-1"] = existing

	svc := newTestVouchService(repo, graph, users, now)

	err := svc.Revoke(context.Background(), "vouch-1", "voucher-1")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Revoke() error = %v, want %v", err, ErrValidation)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/service/ -v -run TestVouchService_Revoke`
Expected: FAIL — `Revoke` method doesn't exist yet.

**Step 3: Write minimal implementation**

Append to `internal/service/vouch.go`:

```go
// Revoke revokes an existing vouch and removes the graph edge. Only the
// original voucher or a moderator/council member can revoke.
func (s *VouchService) Revoke(ctx context.Context, vouchID, requesterID string) error {
	vouch, err := s.repo.GetVouchByID(ctx, vouchID)
	if err != nil {
		return fmt.Errorf("looking up vouch: %w", err)
	}

	if vouch.Status != domain.VouchActive {
		return fmt.Errorf("%w: vouch is already revoked", ErrValidation)
	}

	requester, err := s.users.GetUserByID(ctx, requesterID)
	if err != nil {
		return fmt.Errorf("looking up requester: %w", err)
	}

	isVoucher := vouch.VoucherID == requesterID
	isModerator := requester.CanModerate()
	if !isVoucher && !isModerator {
		return fmt.Errorf("%w: only the voucher or a moderator can revoke", ErrForbidden)
	}

	if err := s.repo.RevokeVouch(ctx, vouchID); err != nil {
		return fmt.Errorf("revoking vouch: %w", err)
	}

	if err := s.graph.RemoveVouchEdge(ctx, vouch.VoucherID, vouch.VoucheeID); err != nil {
		return fmt.Errorf("removing vouch graph edge: %w", err)
	}

	return nil
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/service/ -v -run TestVouchService_Revoke`
Expected: PASS — all 6 Revoke tests pass.

**Step 5: Commit**

```bash
cd /home/jeremy/services/the-bell
git add internal/service/vouch.go internal/service/vouch_test.go
git commit -m "feat: add VouchService.Revoke with authorization (voucher or moderator)"
```

---

### Task 4: Error propagation tests

**Files:**
- Modify: `internal/service/vouch_test.go`

**Step 1: Write error propagation tests**

Append to `internal/service/vouch_test.go`:

```go
func TestVouchService_Vouch_RepoCreateError(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()

	users.users["voucher-1"] = makeUser("voucher-1", domain.RoleMember, 75.0)
	users.users["vouchee-1"] = makeUser("vouchee-1", domain.RolePending, 50.0)

	repo.createErr = errors.New("db write failed")

	svc := newTestVouchService(repo, graph, users, now)

	_, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if err == nil {
		t.Fatal("Vouch() expected error, got nil")
	}
	if !errors.Is(err, repo.createErr) {
		t.Errorf("error should wrap repo error, got: %v", err)
	}
}

func TestVouchService_Vouch_GraphAddError(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()

	users.users["voucher-1"] = makeUser("voucher-1", domain.RoleMember, 75.0)
	users.users["vouchee-1"] = makeUser("vouchee-1", domain.RolePending, 50.0)

	graph.addErr = errors.New("graph unavailable")

	svc := newTestVouchService(repo, graph, users, now)

	_, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if err == nil {
		t.Fatal("Vouch() expected error, got nil")
	}
	if !errors.Is(err, graph.addErr) {
		t.Errorf("error should wrap graph error, got: %v", err)
	}
}

func TestVouchService_Revoke_RepoRevokeError(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()

	users.users["voucher-1"] = makeUser("voucher-1", domain.RoleMember, 75.0)

	repo.vouches["vouch-1"] = &domain.Vouch{
		ID:        "vouch-1",
		VoucherID: "voucher-1",
		VoucheeID: "vouchee-1",
		Status:    domain.VouchActive,
	}
	repo.revokeErr = errors.New("db write failed")

	svc := newTestVouchService(repo, graph, users, now)

	err := svc.Revoke(context.Background(), "vouch-1", "voucher-1")
	if err == nil {
		t.Fatal("Revoke() expected error, got nil")
	}
	if !errors.Is(err, repo.revokeErr) {
		t.Errorf("error should wrap repo error, got: %v", err)
	}
}

func TestVouchService_Revoke_GraphRemoveError(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()

	users.users["voucher-1"] = makeUser("voucher-1", domain.RoleMember, 75.0)

	repo.vouches["vouch-1"] = &domain.Vouch{
		ID:        "vouch-1",
		VoucherID: "voucher-1",
		VoucheeID: "vouchee-1",
		Status:    domain.VouchActive,
	}
	graph.removeErr = errors.New("graph unavailable")

	svc := newTestVouchService(repo, graph, users, now)

	err := svc.Revoke(context.Background(), "vouch-1", "voucher-1")
	if err == nil {
		t.Fatal("Revoke() expected error, got nil")
	}
	if !errors.Is(err, graph.removeErr) {
		t.Errorf("error should wrap graph error, got: %v", err)
	}
}
```

**Step 2: Run tests to verify they pass**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/service/ -v -run TestVouchService`
Expected: PASS — all vouch service tests pass.

**Step 3: Commit**

```bash
cd /home/jeremy/services/the-bell
git add internal/service/vouch_test.go
git commit -m "test: add error propagation tests for VouchService"
```

---

### Task 5: Full test suite and verification

**Step 1: Run all service tests**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/service/ -v -count=1`
Expected: All tests pass (user, post, vouch).

**Step 2: Run full project test suite**

Run: `cd /home/jeremy/services/the-bell && go test ./... -count=1`
Expected: All tests pass across all packages.

**Step 3: Run go vet**

Run: `cd /home/jeremy/services/the-bell && go vet ./...`
Expected: No issues.

**Step 4: Verify build**

Run: `cd /home/jeremy/services/the-bell && go build ./...`
Expected: Clean build.

---

## Edge Cases and Risks

1. **Re-vouching after revocation**: The `GetVouchByPair` query returns vouches in any status. The `vouches` table has `UNIQUE(voucher_id, vouchee_id)`, so only one record per pair can exist. If a vouch is revoked, re-vouching the same pair is blocked by both the service check and the DB constraint. Re-vouching is intentionally not supported — it can be added later as a separate feature (reactivate existing record).

2. **Graph-DB consistency**: The vouch record is created before the graph edge. If `AddVouchEdge` fails after `CreateVouch` succeeds, the vouch exists in the DB but not in the graph. This is acceptable for now — the graph is used for transitive trust analysis (read optimization), not as the source of truth. The DB `vouches` table is authoritative. A future background job could reconcile graph/DB.

3. **Race condition on daily limit**: Two concurrent `Vouch` calls could both read count=2, then both create, resulting in 4 vouches. The DB allows this since there's no constraint. Acceptable for now — exact enforcement is not critical for a daily limit, and the window for the race is tiny.

4. **Promotion timing**: The vouchee is promoted to member after the vouch record and graph edge are both created. If `UpdateUserRole` fails, the vouch still exists but the user stays pending. This is self-correcting — the next vouch attempt for this pair will see the existing vouch, but a different voucher could re-trigger promotion. Alternatively, a reconciliation job could check pending users with active vouches.

5. **Revoke authorization**: Only the original voucher or a moderator/council can revoke. The vouchee cannot revoke vouches they received — this is intentional per trust graph design (you can't remove someone else's expression of trust for you).

6. **Inactive users**: `CanVouch()` checks `IsActive`, so deactivated users cannot vouch even if they have high trust. This is correct behavior.

## Test Strategy

| Test | Type | What it validates |
|------|------|-------------------|
| `TestVouchService_Vouch_Success` | Unit | Happy path: creates vouch, stores in repo, creates graph edge |
| `TestVouchService_Vouch_PromotesPendingToMember` | Unit | Pending vouchee gets promoted to member |
| `TestVouchService_Vouch_NoPromoteIfAlreadyMember` | Unit | Member vouchee is not redundantly promoted |
| `TestVouchService_Vouch_SelfVouch` | Unit | Self-vouch returns ErrValidation |
| `TestVouchService_Vouch_LowTrust` | Unit | Trust < 60 returns ErrForbidden |
| `TestVouchService_Vouch_PendingVoucherForbidden` | Unit | Pending role returns ErrForbidden |
| `TestVouchService_Vouch_BannedVoucherForbidden` | Unit | Banned role returns ErrForbidden |
| `TestVouchService_Vouch_DuplicateVouch` | Unit | Existing pair returns ErrValidation |
| `TestVouchService_Vouch_DailyLimitReached` | Unit | Count >= 3 returns ErrValidation |
| `TestVouchService_Vouch_CycleDetected` | Unit | Graph cycle returns ErrValidation |
| `TestVouchService_Vouch_VoucherNotFound` | Unit | Missing voucher returns error |
| `TestVouchService_Vouch_VoucheeNotFound` | Unit | Missing vouchee returns error |
| `TestVouchService_Vouch_RepoCreateError` | Unit | DB write error propagated |
| `TestVouchService_Vouch_GraphAddError` | Unit | Graph error propagated |
| `TestVouchService_Revoke_ByVoucher` | Unit | Voucher can revoke their own vouch |
| `TestVouchService_Revoke_ByModerator` | Unit | Moderator can revoke any vouch |
| `TestVouchService_Revoke_ByCouncil` | Unit | Council can revoke any vouch |
| `TestVouchService_Revoke_UnauthorizedUser` | Unit | Non-voucher member returns ErrForbidden |
| `TestVouchService_Revoke_NotFound` | Unit | Missing vouch returns ErrNotFound |
| `TestVouchService_Revoke_AlreadyRevoked` | Unit | Revoking revoked vouch returns ErrValidation |
| `TestVouchService_Revoke_RepoRevokeError` | Unit | DB revoke error propagated |
| `TestVouchService_Revoke_GraphRemoveError` | Unit | Graph remove error propagated |
| `go test ./...` | All | Full suite remains green |
| `go vet ./...` | Lint | No static analysis issues |

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// mockVouchRepo is an in-memory VouchRepository for testing.
type mockVouchRepo struct {
	vouches map[string]*domain.Vouch       // keyed by ID
	byPair  map[[2]string]*domain.Vouch    // keyed by [voucher, vouchee]
	counts  map[string]int64               // daily counts by voucherID

	createErr error
	revokeErr error
}

func newMockVouchRepo() *mockVouchRepo {
	return &mockVouchRepo{
		vouches: make(map[string]*domain.Vouch),
		byPair:  make(map[[2]string]*domain.Vouch),
		counts:  make(map[string]int64),
	}
}

func (m *mockVouchRepo) CreateVouch(_ context.Context, vouch *domain.Vouch) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.vouches[vouch.ID] = vouch
	m.byPair[[2]string{vouch.VoucherID, vouch.VoucheeID}] = vouch
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
	v, ok := m.byPair[[2]string{voucherID, voucheeID}]
	if !ok {
		return nil, ErrNotFound
	}
	return v, nil
}

func (m *mockVouchRepo) CountVouchesByVoucherSince(_ context.Context, voucherID string, _ time.Time) (int64, error) {
	return m.counts[voucherID], nil
}

func (m *mockVouchRepo) ListActiveVouchesByVouchee(_ context.Context, voucheeID string) ([]*domain.Vouch, error) {
	var result []*domain.Vouch
	for _, v := range m.vouches {
		if v.VoucheeID == voucheeID && v.Status == domain.VouchActive {
			result = append(result, v)
		}
	}
	return result, nil
}

func (m *mockVouchRepo) ListActiveVouchesByVoucher(_ context.Context, voucherID string) ([]*domain.Vouch, error) {
	var result []*domain.Vouch
	for _, v := range m.vouches {
		if v.VoucherID == voucherID && v.Status == domain.VouchActive {
			result = append(result, v)
		}
	}
	return result, nil
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

// mockGraph is an in-memory GraphQuerier for testing.
type mockGraph struct {
	edges    map[[2]string]bool
	cycles   map[[2]string]bool

	addEdgeErr    error
	removeEdgeErr error
}

func newMockGraph() *mockGraph {
	return &mockGraph{
		edges:  make(map[[2]string]bool),
		cycles: make(map[[2]string]bool),
	}
}

func (m *mockGraph) AddVouchEdge(_ context.Context, voucherID, voucheeID string) error {
	if m.addEdgeErr != nil {
		return m.addEdgeErr
	}
	m.edges[[2]string{voucherID, voucheeID}] = true
	return nil
}

func (m *mockGraph) RemoveVouchEdge(_ context.Context, voucherID, voucheeID string) error {
	if m.removeEdgeErr != nil {
		return m.removeEdgeErr
	}
	delete(m.edges, [2]string{voucherID, voucheeID})
	return nil
}

func (m *mockGraph) HasCyclicVouch(_ context.Context, voucherID, voucheeID string) (bool, error) {
	return m.cycles[[2]string{voucherID, voucheeID}], nil
}

// mockUserGetter is an in-memory UserGetter for testing.
type mockUserGetter struct {
	users         map[string]*domain.User
	updateRoleErr error
	updatedRoles  map[string]domain.Role
}

func newMockUserGetter() *mockUserGetter {
	return &mockUserGetter{
		users:        make(map[string]*domain.User),
		updatedRoles: make(map[string]domain.Role),
	}
}

func (m *mockUserGetter) GetUserByID(_ context.Context, id string) (*domain.User, error) {
	u, ok := m.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	return u, nil
}

func (m *mockUserGetter) UpdateUserRole(_ context.Context, id string, role domain.Role) error {
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

func activeMember(id string, trust float64) *domain.User {
	return &domain.User{
		ID:         id,
		TrustScore: trust,
		Role:       domain.RoleMember,
		IsActive:   true,
	}
}

func pendingUser(id string) *domain.User {
	return &domain.User{
		ID:         id,
		TrustScore: 50.0,
		Role:       domain.RolePending,
		IsActive:   true,
	}
}

var fixedNow = time.Date(2026, 3, 1, 14, 0, 0, 0, time.UTC)

func fixedClock() time.Time { return fixedNow }

// --- Vouch success path ---

func TestVouchService_Vouch_Success(t *testing.T) {
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 80.0)
	users.users["vouchee-1"] = activeMember("vouchee-1", 50.0)

	svc := NewVouchService(repo, graph, users, fixedClock)
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
	if !vouch.CreatedAt.Equal(fixedNow) {
		t.Errorf("CreatedAt = %v, want %v", vouch.CreatedAt, fixedNow)
	}
	// Verify graph edge created
	if !graph.edges[[2]string{"voucher-1", "vouchee-1"}] {
		t.Error("graph edge not created")
	}
	// Verify vouch stored in repo
	if _, ok := repo.vouches[vouch.ID]; !ok {
		t.Error("vouch not stored in repository")
	}
}

func TestVouchService_Vouch_PromotesPendingToMember(t *testing.T) {
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 80.0)
	users.users["vouchee-1"] = pendingUser("vouchee-1")

	svc := NewVouchService(repo, graph, users, fixedClock)
	_, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if err != nil {
		t.Fatalf("Vouch() unexpected error: %v", err)
	}

	if users.users["vouchee-1"].Role != domain.RoleMember {
		t.Errorf("vouchee role = %q, want %q", users.users["vouchee-1"].Role, domain.RoleMember)
	}
	if users.updatedRoles["vouchee-1"] != domain.RoleMember {
		t.Error("UpdateUserRole not called for pending vouchee")
	}
}

func TestVouchService_Vouch_NoRedundantPromotion(t *testing.T) {
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 80.0)
	users.users["vouchee-1"] = activeMember("vouchee-1", 50.0)

	svc := NewVouchService(repo, graph, users, fixedClock)
	_, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if err != nil {
		t.Fatalf("Vouch() unexpected error: %v", err)
	}

	if _, ok := users.updatedRoles["vouchee-1"]; ok {
		t.Error("UpdateUserRole called for already-member vouchee")
	}
}

// --- Vouch validation errors ---

func TestVouchService_Vouch_SelfVouch(t *testing.T) {
	svc := NewVouchService(newMockVouchRepo(), newMockGraph(), newMockUserGetter(), fixedClock)
	_, err := svc.Vouch(context.Background(), "user-1", "user-1")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Vouch() error = %v, want %v", err, ErrValidation)
	}
}

func TestVouchService_Vouch_LowTrust(t *testing.T) {
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 50.0) // below 60 threshold

	svc := NewVouchService(newMockVouchRepo(), newMockGraph(), users, fixedClock)
	_, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Vouch() error = %v, want %v", err, ErrForbidden)
	}
}

func TestVouchService_Vouch_PendingVoucher(t *testing.T) {
	users := newMockUserGetter()
	users.users["voucher-1"] = pendingUser("voucher-1")
	users.users["voucher-1"].TrustScore = 80.0 // high trust but pending role

	svc := NewVouchService(newMockVouchRepo(), newMockGraph(), users, fixedClock)
	_, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Vouch() error = %v, want %v", err, ErrForbidden)
	}
}

func TestVouchService_Vouch_BannedVoucher(t *testing.T) {
	users := newMockUserGetter()
	users.users["voucher-1"] = &domain.User{
		ID: "voucher-1", TrustScore: 80.0, Role: domain.RoleBanned, IsActive: true,
	}

	svc := NewVouchService(newMockVouchRepo(), newMockGraph(), users, fixedClock)
	_, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Vouch() error = %v, want %v", err, ErrForbidden)
	}
}

func TestVouchService_Vouch_VoucherNotFound(t *testing.T) {
	svc := NewVouchService(newMockVouchRepo(), newMockGraph(), newMockUserGetter(), fixedClock)
	_, err := svc.Vouch(context.Background(), "nonexistent", "vouchee-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Vouch() error = %v, want %v", err, ErrNotFound)
	}
}

func TestVouchService_Vouch_VoucheeNotFound(t *testing.T) {
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 80.0)

	svc := NewVouchService(newMockVouchRepo(), newMockGraph(), users, fixedClock)
	_, err := svc.Vouch(context.Background(), "voucher-1", "nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Vouch() error = %v, want %v", err, ErrNotFound)
	}
}

func TestVouchService_Vouch_DuplicatePair(t *testing.T) {
	repo := newMockVouchRepo()
	repo.byPair[[2]string{"voucher-1", "vouchee-1"}] = &domain.Vouch{
		ID: "existing-vouch", VoucherID: "voucher-1", VoucheeID: "vouchee-1",
		Status: domain.VouchActive,
	}
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 80.0)
	users.users["vouchee-1"] = activeMember("vouchee-1", 50.0)

	svc := NewVouchService(repo, newMockGraph(), users, fixedClock)
	_, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Vouch() error = %v, want %v", err, ErrValidation)
	}
}

func TestVouchService_Vouch_DailyLimitReached(t *testing.T) {
	repo := newMockVouchRepo()
	repo.counts["voucher-1"] = 3
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 80.0)
	users.users["vouchee-1"] = activeMember("vouchee-1", 50.0)

	svc := NewVouchService(repo, newMockGraph(), users, fixedClock)
	_, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Vouch() error = %v, want %v", err, ErrValidation)
	}
}

func TestVouchService_Vouch_CycleDetected(t *testing.T) {
	graph := newMockGraph()
	graph.cycles[[2]string{"voucher-1", "vouchee-1"}] = true
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 80.0)
	users.users["vouchee-1"] = activeMember("vouchee-1", 50.0)

	svc := NewVouchService(newMockVouchRepo(), graph, users, fixedClock)
	_, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Vouch() error = %v, want %v", err, ErrValidation)
	}
}

// --- Vouch error propagation ---

func TestVouchService_Vouch_CreateError(t *testing.T) {
	repo := newMockVouchRepo()
	repo.createErr = errors.New("db connection lost")
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 80.0)
	users.users["vouchee-1"] = activeMember("vouchee-1", 50.0)

	svc := NewVouchService(repo, newMockGraph(), users, fixedClock)
	_, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if err == nil {
		t.Fatal("Vouch() expected error, got nil")
	}
}

func TestVouchService_Vouch_GraphAddEdgeError(t *testing.T) {
	graph := newMockGraph()
	graph.addEdgeErr = errors.New("graph unavailable")
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 80.0)
	users.users["vouchee-1"] = activeMember("vouchee-1", 50.0)

	svc := NewVouchService(newMockVouchRepo(), graph, users, fixedClock)
	_, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if err == nil {
		t.Fatal("Vouch() expected error, got nil")
	}
}

// --- Revoke success path ---

func TestVouchService_Revoke_ByVoucher(t *testing.T) {
	repo := newMockVouchRepo()
	graph := newMockGraph()
	graph.edges[[2]string{"voucher-1", "vouchee-1"}] = true
	repo.vouches["vouch-1"] = &domain.Vouch{
		ID: "vouch-1", VoucherID: "voucher-1", VoucheeID: "vouchee-1",
		Status: domain.VouchActive,
	}
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 50.0)

	svc := NewVouchService(repo, graph, users, fixedClock)
	err := svc.Revoke(context.Background(), "vouch-1", "voucher-1")
	if err != nil {
		t.Fatalf("Revoke() unexpected error: %v", err)
	}
	if repo.vouches["vouch-1"].Status != domain.VouchRevoked {
		t.Error("vouch status not revoked")
	}
	if graph.edges[[2]string{"voucher-1", "vouchee-1"}] {
		t.Error("graph edge not removed")
	}
}

func TestVouchService_Revoke_ByModerator(t *testing.T) {
	repo := newMockVouchRepo()
	graph := newMockGraph()
	graph.edges[[2]string{"voucher-1", "vouchee-1"}] = true
	repo.vouches["vouch-1"] = &domain.Vouch{
		ID: "vouch-1", VoucherID: "voucher-1", VoucheeID: "vouchee-1",
		Status: domain.VouchActive,
	}
	users := newMockUserGetter()
	users.users["mod-1"] = &domain.User{
		ID: "mod-1", TrustScore: 80.0, Role: domain.RoleModerator, IsActive: true,
	}

	svc := NewVouchService(repo, graph, users, fixedClock)
	err := svc.Revoke(context.Background(), "vouch-1", "mod-1")
	if err != nil {
		t.Fatalf("Revoke() unexpected error: %v", err)
	}
}

func TestVouchService_Revoke_ByCouncil(t *testing.T) {
	repo := newMockVouchRepo()
	graph := newMockGraph()
	graph.edges[[2]string{"voucher-1", "vouchee-1"}] = true
	repo.vouches["vouch-1"] = &domain.Vouch{
		ID: "vouch-1", VoucherID: "voucher-1", VoucheeID: "vouchee-1",
		Status: domain.VouchActive,
	}
	users := newMockUserGetter()
	users.users["council-1"] = &domain.User{
		ID: "council-1", TrustScore: 90.0, Role: domain.RoleCouncil, IsActive: true,
	}

	svc := NewVouchService(repo, graph, users, fixedClock)
	err := svc.Revoke(context.Background(), "vouch-1", "council-1")
	if err != nil {
		t.Fatalf("Revoke() unexpected error: %v", err)
	}
}

// --- Revoke validation / authorization errors ---

func TestVouchService_Revoke_Unauthorized(t *testing.T) {
	repo := newMockVouchRepo()
	repo.vouches["vouch-1"] = &domain.Vouch{
		ID: "vouch-1", VoucherID: "voucher-1", VoucheeID: "vouchee-1",
		Status: domain.VouchActive,
	}
	users := newMockUserGetter()
	users.users["other-user"] = activeMember("other-user", 50.0)

	svc := NewVouchService(repo, newMockGraph(), users, fixedClock)
	err := svc.Revoke(context.Background(), "vouch-1", "other-user")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Revoke() error = %v, want %v", err, ErrForbidden)
	}
}

func TestVouchService_Revoke_NotFound(t *testing.T) {
	svc := NewVouchService(newMockVouchRepo(), newMockGraph(), newMockUserGetter(), fixedClock)
	err := svc.Revoke(context.Background(), "nonexistent", "user-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Revoke() error = %v, want %v", err, ErrNotFound)
	}
}

func TestVouchService_Revoke_AlreadyRevoked(t *testing.T) {
	repo := newMockVouchRepo()
	repo.vouches["vouch-1"] = &domain.Vouch{
		ID: "vouch-1", VoucherID: "voucher-1", VoucheeID: "vouchee-1",
		Status: domain.VouchRevoked,
	}

	svc := NewVouchService(repo, newMockGraph(), newMockUserGetter(), fixedClock)
	err := svc.Revoke(context.Background(), "vouch-1", "voucher-1")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Revoke() error = %v, want %v", err, ErrValidation)
	}
}

// --- Revoke error propagation ---

func TestVouchService_Revoke_RepoError(t *testing.T) {
	repo := newMockVouchRepo()
	repo.vouches["vouch-1"] = &domain.Vouch{
		ID: "vouch-1", VoucherID: "voucher-1", VoucheeID: "vouchee-1",
		Status: domain.VouchActive,
	}
	repo.revokeErr = errors.New("db write failed")
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 50.0)

	svc := NewVouchService(repo, newMockGraph(), users, fixedClock)
	err := svc.Revoke(context.Background(), "vouch-1", "voucher-1")
	if err == nil {
		t.Fatal("Revoke() expected error, got nil")
	}
}

func TestVouchService_Revoke_GraphRemoveError(t *testing.T) {
	repo := newMockVouchRepo()
	repo.vouches["vouch-1"] = &domain.Vouch{
		ID: "vouch-1", VoucherID: "voucher-1", VoucheeID: "vouchee-1",
		Status: domain.VouchActive,
	}
	graph := newMockGraph()
	graph.removeEdgeErr = errors.New("graph unavailable")
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 50.0)

	svc := NewVouchService(repo, graph, users, fixedClock)
	err := svc.Revoke(context.Background(), "vouch-1", "voucher-1")
	if err == nil {
		t.Fatal("Revoke() expected error, got nil")
	}
}

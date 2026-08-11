package service

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// mockVouchRepo is an in-memory VouchRepository for testing. Counting is
// derived from the stored vouches rather than a preset total, so the `since`
// argument the service computes is actually applied.
type mockVouchRepo struct {
	vouches map[string]*domain.Vouch    // keyed by ID
	byPair  map[[2]string]*domain.Vouch // keyed by [voucher, vouchee]

	createErr error
	revokeErr error
}

func newMockVouchRepo() *mockVouchRepo {
	return &mockVouchRepo{
		vouches: make(map[string]*domain.Vouch),
		byPair:  make(map[[2]string]*domain.Vouch),
	}
}

// seedVouch records a vouch that already exists at the given time, so the
// daily-limit window has something to count.
func (m *mockVouchRepo) seedVouch(voucherID, voucheeID string, createdAt time.Time) {
	v := &domain.Vouch{
		ID:        voucherID + "->" + voucheeID,
		VoucherID: voucherID,
		VoucheeID: voucheeID,
		Status:    domain.VouchActive,
		CreatedAt: createdAt,
	}
	m.vouches[v.ID] = v
	m.byPair[[2]string{voucherID, voucheeID}] = v
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

// CountVouchesByVoucherSince mirrors the SQL: created_at >= since.
func (m *mockVouchRepo) CountVouchesByVoucherSince(_ context.Context, voucherID string, since time.Time) (int64, error) {
	var count int64
	for _, v := range m.vouches {
		if v.VoucherID == voucherID && !v.CreatedAt.Before(since) {
			count++
		}
	}
	return count, nil
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
	edges  map[[2]string]bool
	cycles map[[2]string]bool

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
	// vanishing users disappear after their first successful lookup, standing
	// in for an account removed between the two reads inside one Vouch call.
	vanishing map[string]bool
}

func newMockUserGetter() *mockUserGetter {
	return &mockUserGetter{
		users:        make(map[string]*domain.User),
		updatedRoles: make(map[string]domain.Role),
		vanishing:    make(map[string]bool),
	}
}

func (m *mockUserGetter) GetUserByID(_ context.Context, id string) (*domain.User, error) {
	u, ok := m.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	if m.vanishing[id] {
		delete(m.users, id)
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
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 80.0)
	users.users["vouchee-1"] = activeMember("vouchee-1", 50.0)
	for i := 0; i < dailyVouchLimit; i++ {
		repo.seedVouch("voucher-1", "earlier-today-"+string(rune('a'+i)), fixedNow.Add(-time.Hour))
	}

	svc := NewVouchService(repo, newMockGraph(), users, fixedClock)
	_, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Vouch() error = %v, want %v", err, ErrValidation)
	}
}

// The daily limit counts from local midnight, so yesterday's vouches must not
// be charged against today's allowance. Before startOfDay was extracted, the
// mock discarded the `since` argument and this boundary was untested.
func TestVouchService_Vouch_DailyLimitResetsAtMidnight(t *testing.T) {
	// A minute past midnight, having spent the whole allowance a minute before.
	justAfterMidnight := time.Date(2026, 3, 2, 0, 1, 0, 0, time.UTC)
	justBeforeMidnight := time.Date(2026, 3, 1, 23, 59, 0, 0, time.UTC)

	repo := newMockVouchRepo()
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 80.0)
	users.users["vouchee-1"] = activeMember("vouchee-1", 50.0)
	for i := 0; i < dailyVouchLimit; i++ {
		repo.seedVouch("voucher-1", "yesterday-"+string(rune('a'+i)), justBeforeMidnight)
	}

	svc := NewVouchService(repo, newMockGraph(), users, func() time.Time { return justAfterMidnight })
	if _, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1"); err != nil {
		t.Fatalf("Vouch() unexpected error: %v", err)
	}
}

// The mirror of the above: vouches made just after midnight are still charged
// against the same day right up until the next one.
func TestVouchService_Vouch_DailyLimitCoversTheWholeDay(t *testing.T) {
	justAfterMidnight := time.Date(2026, 3, 1, 0, 1, 0, 0, time.UTC)
	justBeforeMidnight := time.Date(2026, 3, 1, 23, 59, 0, 0, time.UTC)

	repo := newMockVouchRepo()
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 80.0)
	users.users["vouchee-1"] = activeMember("vouchee-1", 50.0)
	for i := 0; i < dailyVouchLimit; i++ {
		repo.seedVouch("voucher-1", "this-morning-"+string(rune('a'+i)), justAfterMidnight)
	}

	svc := NewVouchService(repo, newMockGraph(), users, func() time.Time { return justBeforeMidnight })
	_, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Vouch() error = %v, want %v", err, ErrValidation)
	}
}

// The allowance is per voucher; one person exhausting theirs must not block
// anyone else.
func TestVouchService_Vouch_DailyLimitIsPerVoucher(t *testing.T) {
	repo := newMockVouchRepo()
	users := newMockUserGetter()
	users.users["busy-voucher"] = activeMember("busy-voucher", 80.0)
	users.users["voucher-1"] = activeMember("voucher-1", 80.0)
	users.users["vouchee-1"] = activeMember("vouchee-1", 50.0)
	for i := 0; i < dailyVouchLimit; i++ {
		repo.seedVouch("busy-voucher", "vouchee-"+string(rune('a'+i)), fixedNow.Add(-time.Hour))
	}

	svc := NewVouchService(repo, newMockGraph(), users, fixedClock)
	if _, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1"); err != nil {
		t.Fatalf("Vouch() unexpected error: %v", err)
	}
}

// One under the limit is still allowed; the check is >=, not >.
func TestVouchService_Vouch_JustUnderDailyLimit(t *testing.T) {
	repo := newMockVouchRepo()
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 80.0)
	users.users["vouchee-1"] = activeMember("vouchee-1", 50.0)
	for i := 0; i < dailyVouchLimit-1; i++ {
		repo.seedVouch("voucher-1", "earlier-today-"+string(rune('a'+i)), fixedNow.Add(-time.Hour))
	}

	svc := NewVouchService(repo, newMockGraph(), users, fixedClock)
	if _, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1"); err != nil {
		t.Fatalf("Vouch() unexpected error: %v", err)
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

// The promotion that follows a vouch is best-effort: the vouch and its graph
// edge are already committed, so losing the vouchee between the two reads must
// still return the vouch rather than an error the caller cannot act on.
func TestVouchService_Vouch_PromotionLookupFailureStillReturnsTheVouch(t *testing.T) {
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 80.0)
	users.users["vouchee-1"] = pendingUser("vouchee-1")
	users.vanishing["vouchee-1"] = true

	svc := NewVouchService(repo, graph, users, fixedClock)
	vouch, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if err != nil {
		t.Fatalf("Vouch() unexpected error: %v", err)
	}
	if vouch == nil {
		t.Fatal("Vouch() = nil, want the committed vouch")
	}
	if !graph.edges[[2]string{"voucher-1", "vouchee-1"}] {
		t.Error("graph edge missing; the vouch itself should have committed")
	}
	if _, ok := users.updatedRoles["vouchee-1"]; ok {
		t.Error("UpdateUserRole was called for a vouchee that could not be read")
	}
}

func TestVouchService_Revoke_ActorNotFound(t *testing.T) {
	repo := newMockVouchRepo()
	repo.vouches["vouch-1"] = &domain.Vouch{
		ID: "vouch-1", VoucherID: "voucher-1", VoucheeID: "vouchee-1",
		Status: domain.VouchActive,
	}

	svc := NewVouchService(repo, newMockGraph(), newMockUserGetter(), fixedClock)
	err := svc.Revoke(context.Background(), "vouch-1", "ghost")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Revoke() error = %v, want %v", err, ErrNotFound)
	}
	if repo.vouches["vouch-1"].Status != domain.VouchActive {
		t.Error("vouch was revoked despite the actor lookup failing")
	}
}

// --- Listing vouches ---

// The two list methods differ only in direction, and their repository methods
// have near-identical names. This pins that each one asks for the right side of
// the edge.
func TestVouchService_ListVouches_DirectionsDoNotCross(t *testing.T) {
	repo := newMockVouchRepo()
	repo.seedVouch("alice", "bob", fixedNow)
	repo.seedVouch("carol", "alice", fixedNow)

	svc := NewVouchService(repo, newMockGraph(), newMockUserGetter(), fixedClock)

	given, err := svc.ListGivenVouches(context.Background(), "alice")
	if err != nil {
		t.Fatalf("ListGivenVouches() unexpected error: %v", err)
	}
	if len(given) != 1 || given[0].VoucheeID != "bob" {
		t.Errorf("ListGivenVouches(alice) = %v, want the alice->bob vouch", given)
	}

	received, err := svc.ListReceivedVouches(context.Background(), "alice")
	if err != nil {
		t.Fatalf("ListReceivedVouches() unexpected error: %v", err)
	}
	if len(received) != 1 || received[0].VoucherID != "carol" {
		t.Errorf("ListReceivedVouches(alice) = %v, want the carol->alice vouch", received)
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

// --- Trust recalculation enqueueing ---

// A vouch changes the vouchee's voucher component — CalcVoucherScore counts
// vouches RECEIVED — so the vouchee must be requeued. The voucher is requeued
// too, per the design doc's recalculation triggers.
func TestVouchService_Vouch_EnqueuesTrustRecalc(t *testing.T) {
	repo := newMockVouchRepo()
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 80.0)
	users.users["vouchee-1"] = activeMember("vouchee-1", 50.0)
	queue := &fakeTrustQueue{}

	svc := NewVouchService(repo, newMockGraph(), users, fixedClock)
	svc.SetTrustQueue(queue)

	if _, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1"); err != nil {
		t.Fatalf("Vouch() unexpected error: %v", err)
	}

	if !queue.contains("vouchee-1") {
		t.Errorf("vouchee not enqueued: %v", queue.enqueued)
	}
	if !queue.contains("voucher-1") {
		t.Errorf("voucher not enqueued: %v", queue.enqueued)
	}
}

// Revoking removes an endorsement, which lowers the vouchee's voucher
// component just as gaining one raised it.
func TestVouchService_Revoke_EnqueuesTrustRecalc(t *testing.T) {
	repo := newMockVouchRepo()
	repo.vouches["vouch-1"] = &domain.Vouch{
		ID: "vouch-1", VoucherID: "voucher-1", VoucheeID: "vouchee-1",
		Status: domain.VouchActive,
	}
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 80.0)
	queue := &fakeTrustQueue{}

	svc := NewVouchService(repo, newMockGraph(), users, fixedClock)
	svc.SetTrustQueue(queue)

	if err := svc.Revoke(context.Background(), "vouch-1", "voucher-1"); err != nil {
		t.Fatalf("Revoke() unexpected error: %v", err)
	}

	if !queue.contains("vouchee-1") {
		t.Errorf("vouchee not enqueued: %v", queue.enqueued)
	}
	if !queue.contains("voucher-1") {
		t.Errorf("voucher not enqueued: %v", queue.enqueued)
	}
}

// A deployment without Redis leaves the queue unset; vouching must still work.
func TestVouchService_Vouch_NoQueueConfigured(t *testing.T) {
	repo := newMockVouchRepo()
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 80.0)
	users.users["vouchee-1"] = activeMember("vouchee-1", 50.0)

	svc := NewVouchService(repo, newMockGraph(), users, fixedClock)
	if _, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1"); err != nil {
		t.Fatalf("Vouch() unexpected error: %v", err)
	}
}

// The vouch and its graph edge are already committed when the enqueue runs, so
// a broken queue must not fail the vouch.
func TestVouchService_Vouch_EnqueueFailureIsNotFatal(t *testing.T) {
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 80.0)
	users.users["vouchee-1"] = activeMember("vouchee-1", 50.0)

	svc := NewVouchService(repo, graph, users, fixedClock)
	svc.SetTrustQueue(&fakeTrustQueue{err: errors.New("redis unavailable")})
	svc.logger = discardLogger()

	vouch, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if err != nil {
		t.Fatalf("Vouch() error = %v, want the vouch to survive a queue failure", err)
	}
	if vouch == nil {
		t.Fatal("Vouch() = nil, want the committed vouch")
	}
	if !graph.edges[[2]string{"voucher-1", "vouchee-1"}] {
		t.Error("graph edge missing; the vouch itself should have committed")
	}
}

// Likewise for revocation: the edge is already gone by the time we enqueue.
func TestVouchService_Revoke_EnqueueFailureIsNotFatal(t *testing.T) {
	repo := newMockVouchRepo()
	repo.vouches["vouch-1"] = &domain.Vouch{
		ID: "vouch-1", VoucherID: "voucher-1", VoucheeID: "vouchee-1",
		Status: domain.VouchActive,
	}
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 80.0)

	svc := NewVouchService(repo, newMockGraph(), users, fixedClock)
	svc.SetTrustQueue(&fakeTrustQueue{err: errors.New("redis unavailable")})
	svc.logger = discardLogger()

	if err := svc.Revoke(context.Background(), "vouch-1", "voucher-1"); err != nil {
		t.Fatalf("Revoke() error = %v, want the revocation to survive a queue failure", err)
	}
	if repo.vouches["vouch-1"].Status != domain.VouchRevoked {
		t.Error("vouch was not revoked")
	}
}

// A vouch rejected by validation changes nobody's score.
func TestVouchService_Vouch_RejectedVouchEnqueuesNothing(t *testing.T) {
	repo := newMockVouchRepo()
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 50.0) // below the vouching threshold
	queue := &fakeTrustQueue{}

	svc := NewVouchService(repo, newMockGraph(), users, fixedClock)
	svc.SetTrustQueue(queue)

	if _, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Vouch() error = %v, want %v", err, ErrForbidden)
	}
	if len(queue.enqueued) != 0 {
		t.Errorf("enqueued %v, want nothing for a rejected vouch", queue.enqueued)
	}
}

// --- Revocation penalty (design doc: -3 for 30 days) ---

// revocableVouch builds the common fixture: an active vouch, its graph edge,
// and the voucher.
func revocableVouch(voucherTrust float64) (*mockVouchRepo, *mockGraph, *mockUserGetter) {
	repo := newMockVouchRepo()
	graph := newMockGraph()
	graph.edges[[2]string{"voucher-1", "vouchee-1"}] = true
	repo.vouches["vouch-1"] = &domain.Vouch{
		ID: "vouch-1", VoucherID: "voucher-1", VoucheeID: "vouchee-1",
		Status: domain.VouchActive,
	}
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", voucherTrust)
	return repo, graph, users
}

func TestVouchService_Revoke_ChargesTheVoucherThreePointsForThirtyDays(t *testing.T) {
	repo, graph, users := revocableVouch(50.0)
	penalties := newMockPenaltyRepo()

	svc := NewVouchService(repo, graph, users, fixedClock)
	svc.SetPenaltyRepository(penalties)

	if err := svc.Revoke(context.Background(), "vouch-1", "voucher-1"); err != nil {
		t.Fatalf("Revoke() unexpected error: %v", err)
	}

	if len(penalties.penalties) != 1 {
		t.Fatalf("wrote %d penalties, want 1", len(penalties.penalties))
	}
	p := penalties.penalties[0]

	// The voucher pays, not the person they vouched for: the vouchee already
	// loses the endorsement, and charging them would punish being dropped.
	if p.UserID != "voucher-1" {
		t.Errorf("penalty charged to %q, want the voucher %q", p.UserID, "voucher-1")
	}
	if p.PenaltyAmount != revocationPenaltyPoints {
		t.Errorf("penalty = %v, want %v", p.PenaltyAmount, revocationPenaltyPoints)
	}
	// Nothing moderated anything, so there is no action to point at.
	if p.ModerationActionID != "" {
		t.Errorf("moderation action = %q, want empty (no moderation action exists)", p.ModerationActionID)
	}
	if !p.CreatedAt.Equal(fixedNow) {
		t.Errorf("created_at = %v, want %v", p.CreatedAt, fixedNow)
	}
	if p.DecaysAt == nil {
		t.Fatal("penalty is permanent; it must decay after 30 days")
	}
	if want := fixedNow.AddDate(0, 0, revocationPenaltyDecayDays); !p.DecaysAt.Equal(want) {
		t.Errorf("decays_at = %v, want %v", p.DecaysAt, want)
	}
	// CreatedAt and DecaysAt must come from one clock read, because the decay
	// window is recovered as the difference between them.
	if days := penaltyDecayDays(*p); days != revocationPenaltyDecayDays {
		t.Errorf("recovered decay window = %d days, want %d", days, revocationPenaltyDecayDays)
	}
}

// A revocation penalty is a self-inflicted cost on one person. Moderation
// penalties propagate to the offender's vouchers; this one must not, or
// withdrawing one endorsement would quietly dock everyone who endorsed you.
func TestVouchService_Revoke_PenaltyDoesNotPropagate(t *testing.T) {
	repo, graph, users := revocableVouch(50.0)
	penalties := newMockPenaltyRepo()

	svc := NewVouchService(repo, graph, users, fixedClock)
	svc.SetPenaltyRepository(penalties)

	if err := svc.Revoke(context.Background(), "vouch-1", "voucher-1"); err != nil {
		t.Fatalf("Revoke() unexpected error: %v", err)
	}

	if len(penalties.penalties) != 1 {
		t.Fatalf("wrote %d penalty rows, want exactly 1 (the penalty propagated)", len(penalties.penalties))
	}
	if got := penalties.penalties[0].HopDepth; got != 0 {
		t.Errorf("hop depth = %d, want 0 (a propagated penalty sits deeper)", got)
	}
}

// Moderators and council may revoke anyone's vouch, and doing so costs nobody
// anything — not the moderator, who is doing the job, and not the voucher, who
// did not choose to withdraw. The asymmetry with self-revocation is deliberate:
// the penalty exists to make vouch-and-revoke gaming expensive, and the gamer
// is always the voucher acting on their own vouch. A moderator who thinks the
// voucher deserves a penalty takes a moderation action, which carries its own.
func TestVouchService_Revoke_ByModeratorPenalisesNobody(t *testing.T) {
	tests := []struct {
		name string
		role domain.Role
	}{
		{"moderator", domain.RoleModerator},
		{"council", domain.RoleCouncil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, graph, users := revocableVouch(50.0)
			users.users["actor-1"] = &domain.User{
				ID: "actor-1", TrustScore: 80.0, Role: tt.role, IsActive: true,
			}
			penalties := newMockPenaltyRepo()

			svc := NewVouchService(repo, graph, users, fixedClock)
			svc.SetPenaltyRepository(penalties)

			if err := svc.Revoke(context.Background(), "vouch-1", "actor-1"); err != nil {
				t.Fatalf("Revoke() unexpected error: %v", err)
			}

			if len(penalties.penalties) != 0 {
				t.Fatalf("wrote %d penalties, want 0: %+v", len(penalties.penalties), penalties.penalties[0])
			}
			// The revocation itself must still have happened.
			if repo.vouches["vouch-1"].Status != domain.VouchRevoked {
				t.Error("vouch was not revoked")
			}
			if graph.edges[[2]string{"voucher-1", "vouchee-1"}] {
				t.Error("graph edge was not removed")
			}
		})
	}
}

// The penalty is deliberately small, and this pins how small in the terms that
// actually matter: what one revocation costs a composite trust score.
//
// Moderation is 30% of the composite, so -3 moderation points is -0.9 overall —
// under a single point, and fully recovered in 30 days.
func TestVouchService_Revoke_CostsUnderOneCompositePoint(t *testing.T) {
	repo, graph, users := revocableVouch(65.0)
	penalties := newMockPenaltyRepo()

	svc := NewVouchService(repo, graph, users, fixedClock)
	svc.SetPenaltyRepository(penalties)

	if err := svc.Revoke(context.Background(), "vouch-1", "voucher-1"); err != nil {
		t.Fatalf("Revoke() unexpected error: %v", err)
	}

	active := []ActivePenalty{toActivePenalty(*penalties.penalties[0])}
	penalised := CalcModerationScore(active, fixedNow)

	// Hold the other three components fixed; only the moderation one moves.
	const tenure, activity, voucher = 80.0, 80.0, 80.0
	before := CompositeScore(tenure, activity, voucher, 100)
	after := CompositeScore(tenure, activity, voucher, penalised)
	cost := before - after

	if cost >= 1.0 {
		t.Errorf("one revocation costs %v composite points; the penalty is meant to be small enough to ignore", cost)
	}
	if math.Abs(cost-0.9) > 0.0001 {
		t.Errorf("revocation cost = %v composite points, want 0.9 (3 penalty points at the 30%% moderation weight)", cost)
	}

	// A voucher with any real margin above the threshold keeps vouching.
	stillAble := &domain.User{
		ID: "voucher-1", IsActive: true, Role: domain.RoleMember,
		TrustScore: 65.0 - cost,
	}
	if !stillAble.CanVouch() {
		t.Errorf("a voucher at 65 lost the ability to vouch after one revocation (now %v)", stillAble.TrustScore)
	}
}

// The boundary case, documented rather than asserted away: because the penalty
// costs 0.9 composite points, a voucher sitting between the threshold and
// threshold+0.9 does drop below it and cannot vouch again until the penalty
// decays. The design doc promises the penalty is "small enough that revoking a
// bad actor is still clearly worth it", not that it can never cross a
// threshold, and someone at exactly 60.0 is marginal by definition.
//
// This is pinned so that the narrow band is a known, deliberate consequence
// rather than a surprise discovered in production.
func TestVouchService_Revoke_MarginalVoucherFallsBelowTheThreshold(t *testing.T) {
	const revocationCompositeCost = revocationPenaltyPoints * 0.30

	atThreshold := &domain.User{
		ID: "voucher-1", IsActive: true, Role: domain.RoleMember,
		TrustScore: domain.VouchingThreshold - revocationCompositeCost,
	}
	if atThreshold.CanVouch() {
		t.Errorf("expected a voucher starting at exactly the threshold to fall below it (now %v)", atThreshold.TrustScore)
	}

	// Someone who started just above the band lands just above the threshold.
	justClear := &domain.User{
		ID: "voucher-2", IsActive: true, Role: domain.RoleMember,
		TrustScore: (domain.VouchingThreshold + revocationCompositeCost + 0.01) - revocationCompositeCost,
	}
	if !justClear.CanVouch() {
		t.Errorf("a voucher just above the band lost the ability to vouch (now %v)", justClear.TrustScore)
	}
}

// "Small enough that revoking a bad actor is still clearly worth it" is a
// comparison, so this makes the comparison rather than asserting "small" in the
// abstract: what one revocation costs the voucher, against what staying
// attached to a vouchee who is later moderated costs them.
//
// Both sides are computed from the real weights and the real propagation plan,
// so the test tracks the model instead of restating today's numbers.
func TestVouchService_Revoke_CostsFarLessThanStandingByABadVouch(t *testing.T) {
	// The cost of losing moderation points, expressed in composite points.
	// Holding the other three components fixed isolates the moderation weight.
	compositeCost := func(moderationPoints float64) float64 {
		const tenure, activity, voucher = 80.0, 80.0, 80.0
		return CompositeScore(tenure, activity, voucher, 100) -
			CompositeScore(tenure, activity, voucher, 100-moderationPoints)
	}

	revokeCost := compositeCost(revocationPenaltyPoints)

	tests := []struct {
		name     string
		severity int
	}{
		{"vouchee muted", 3},
		{"vouchee suspended", 4},
		{"vouchee banned", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The voucher sits one hop from the vouchee, so a moderation action
			// against the vouchee propagates back to them.
			specs := planPropagatedPenalties("vouchee-1", policyFor(t, tt.severity), map[string]int{"voucher-1": 1})
			if len(specs) != 1 {
				t.Fatalf("planned %d propagated penalties, want 1", len(specs))
			}
			stayCost := compositeCost(specs[0].Amount)

			if stayCost <= revokeCost {
				t.Fatalf("standing by a bad vouch costs %.2f composite points and revoking costs %.2f; revoking must be the cheaper choice",
					stayCost, revokeCost)
			}
			// "Clearly" worth it, not marginally: the gap should be large enough
			// that no voucher has to weigh it up.
			if ratio := stayCost / revokeCost; ratio < 4 {
				t.Errorf("standing by a bad vouch costs only %.1fx revoking (%.2f vs %.2f); the deterrent is too close to the cost",
					ratio, stayCost, revokeCost)
			}
		})
	}
}

// The penalty must fade. A permanent -3 for withdrawing an endorsement would
// accumulate over a member's lifetime into a real punishment.
func TestVouchService_Revoke_PenaltyDecaysToNothingAfterThirtyDays(t *testing.T) {
	repo, graph, users := revocableVouch(50.0)
	penalties := newMockPenaltyRepo()

	svc := NewVouchService(repo, graph, users, fixedClock)
	svc.SetPenaltyRepository(penalties)

	if err := svc.Revoke(context.Background(), "vouch-1", "voucher-1"); err != nil {
		t.Fatalf("Revoke() unexpected error: %v", err)
	}
	active := []ActivePenalty{toActivePenalty(*penalties.penalties[0])}

	dayZero := CalcModerationScore(active, fixedNow)
	dayFifteen := CalcModerationScore(active, fixedNow.AddDate(0, 0, 15))
	dayThirtyOne := CalcModerationScore(active, fixedNow.AddDate(0, 0, 31))

	if dayZero >= 100 {
		t.Errorf("day 0 moderation score = %v, want the full penalty applied", dayZero)
	}
	if !(dayZero < dayFifteen && dayFifteen < dayThirtyOne) {
		t.Errorf("penalty does not ramp off linearly: day0=%v day15=%v day31=%v", dayZero, dayFifteen, dayThirtyOne)
	}
	if dayThirtyOne != 100 {
		t.Errorf("day 31 moderation score = %v, want 100 (the penalty should be gone)", dayThirtyOne)
	}
}

// A deployment with no penalty store still revokes; the revocation simply
// carries no cost, the same way an absent trust queue skips recalculation.
func TestVouchService_Revoke_WithoutPenaltyStoreStillRevokes(t *testing.T) {
	repo, graph, users := revocableVouch(50.0)

	svc := NewVouchService(repo, graph, users, fixedClock)

	if err := svc.Revoke(context.Background(), "vouch-1", "voucher-1"); err != nil {
		t.Fatalf("Revoke() unexpected error: %v", err)
	}
	if repo.vouches["vouch-1"].Status != domain.VouchRevoked {
		t.Error("vouch was not revoked")
	}
}

// A revocation that already happened must not be undone because the penalty
// could not be recorded.
func TestVouchService_Revoke_PenaltyWriteFailureDoesNotFailTheRevocation(t *testing.T) {
	repo, graph, users := revocableVouch(50.0)
	penalties := newMockPenaltyRepo()
	penalties.createErr = errors.New("penalty store unavailable")

	svc := NewVouchService(repo, graph, users, fixedClock)
	svc.SetPenaltyRepository(penalties)

	if err := svc.Revoke(context.Background(), "vouch-1", "voucher-1"); err != nil {
		t.Fatalf("Revoke() returned %v; a failed penalty write must not undo the revocation", err)
	}
	if repo.vouches["vouch-1"].Status != domain.VouchRevoked {
		t.Error("vouch was not revoked")
	}
}

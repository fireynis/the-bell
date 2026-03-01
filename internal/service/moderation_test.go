package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
)

// mockPenaltyRepo is an in-memory PenaltyRepository for testing.
type mockPenaltyRepo struct {
	penalties []*domain.TrustPenalty
	createErr error
	failAfter int // fail after this many successful creates (-1 = never fail)
}

func newMockPenaltyRepo() *mockPenaltyRepo {
	return &mockPenaltyRepo{failAfter: -1}
}

func (m *mockPenaltyRepo) CreateTrustPenalty(_ context.Context, p *domain.TrustPenalty) error {
	if m.failAfter >= 0 && len(m.penalties) >= m.failAfter {
		return m.createErr
	}
	if m.createErr != nil && m.failAfter < 0 {
		return m.createErr
	}
	m.penalties = append(m.penalties, p)
	return nil
}

// mockPenaltyGraph is an in-memory PenaltyGraphQuerier for testing.
type mockPenaltyGraph struct {
	vouchers map[string]int // userID -> min hop depth
	err      error
}

func newMockPenaltyGraph() *mockPenaltyGraph {
	return &mockPenaltyGraph{vouchers: make(map[string]int)}
}

func (m *mockPenaltyGraph) FindVouchersWithDepth(_ context.Context, _ string, _ int) (map[string]int, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.vouchers, nil
}

// --- Severity validation ---

func TestModerationService_PropagatePenalties_SeverityTooLow(t *testing.T) {
	svc := NewModerationService(newMockPenaltyRepo(), newMockPenaltyGraph(), fixedClock)
	_, err := svc.PropagatePenalties(context.Background(), "action-1", "user-1", 0)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("PropagatePenalties() error = %v, want %v", err, ErrValidation)
	}
}

func TestModerationService_PropagatePenalties_SeverityTooHigh(t *testing.T) {
	svc := NewModerationService(newMockPenaltyRepo(), newMockPenaltyGraph(), fixedClock)
	_, err := svc.PropagatePenalties(context.Background(), "action-1", "user-1", 6)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("PropagatePenalties() error = %v, want %v", err, ErrValidation)
	}
}

// --- Success paths ---

func TestModerationService_PropagatePenalties_Severity1_OneHop(t *testing.T) {
	repo := newMockPenaltyRepo()
	graph := newMockPenaltyGraph()
	graph.vouchers["voucher-a"] = 1

	svc := NewModerationService(repo, graph, fixedClock)
	results, err := svc.PropagatePenalties(context.Background(), "action-1", "target-1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d penalties, want 2", len(results))
	}

	// Direct penalty
	direct := results[0]
	if direct.UserID != "target-1" {
		t.Errorf("direct.UserID = %q, want %q", direct.UserID, "target-1")
	}
	if direct.HopDepth != 0 {
		t.Errorf("direct.HopDepth = %d, want 0", direct.HopDepth)
	}
	if !approxEqual(direct.PenaltyAmount, 5.0) {
		t.Errorf("direct.PenaltyAmount = %v, want 5.0", direct.PenaltyAmount)
	}

	// Propagated penalty at depth 1: 5.0 * 0.50^1 = 2.5
	prop := results[1]
	if prop.UserID != "voucher-a" {
		t.Errorf("propagated.UserID = %q, want %q", prop.UserID, "voucher-a")
	}
	if prop.HopDepth != 1 {
		t.Errorf("propagated.HopDepth = %d, want 1", prop.HopDepth)
	}
	if !approxEqual(prop.PenaltyAmount, 2.5) {
		t.Errorf("propagated.PenaltyAmount = %v, want 2.5", prop.PenaltyAmount)
	}

	// DecaysAt should be now + 90 days
	wantDecay := fixedNow.AddDate(0, 0, 90)
	if direct.DecaysAt == nil || !direct.DecaysAt.Equal(wantDecay) {
		t.Errorf("direct.DecaysAt = %v, want %v", direct.DecaysAt, wantDecay)
	}
	if prop.DecaysAt == nil || !prop.DecaysAt.Equal(wantDecay) {
		t.Errorf("propagated.DecaysAt = %v, want %v", prop.DecaysAt, wantDecay)
	}
}

func TestModerationService_PropagatePenalties_Severity3_TwoHops(t *testing.T) {
	repo := newMockPenaltyRepo()
	graph := newMockPenaltyGraph()
	graph.vouchers["voucher-a"] = 1
	graph.vouchers["voucher-b"] = 2

	svc := NewModerationService(repo, graph, fixedClock)
	results, err := svc.PropagatePenalties(context.Background(), "action-1", "target-1", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("got %d penalties, want 3", len(results))
	}

	// Direct: 25.0
	if !approxEqual(results[0].PenaltyAmount, 25.0) {
		t.Errorf("direct penalty = %v, want 25.0", results[0].PenaltyAmount)
	}

	// Find propagated penalties by user
	penaltyByUser := make(map[string]domain.TrustPenalty)
	for _, p := range results[1:] {
		penaltyByUser[p.UserID] = p
	}

	// Depth 1: 25.0 * 0.60^1 = 15.0
	if pa, ok := penaltyByUser["voucher-a"]; !ok {
		t.Error("missing penalty for voucher-a")
	} else if !approxEqual(pa.PenaltyAmount, 15.0) {
		t.Errorf("voucher-a penalty = %v, want 15.0", pa.PenaltyAmount)
	}

	// Depth 2: 25.0 * 0.60^2 = 9.0
	if pb, ok := penaltyByUser["voucher-b"]; !ok {
		t.Error("missing penalty for voucher-b")
	} else if !approxEqual(pb.PenaltyAmount, 9.0) {
		t.Errorf("voucher-b penalty = %v, want 9.0", pb.PenaltyAmount)
	}

	// DecaysAt = now + 270 days
	wantDecay := fixedNow.AddDate(0, 0, 270)
	if results[0].DecaysAt == nil || !results[0].DecaysAt.Equal(wantDecay) {
		t.Errorf("DecaysAt = %v, want %v", results[0].DecaysAt, wantDecay)
	}
}

func TestModerationService_PropagatePenalties_Severity5_Permanent(t *testing.T) {
	repo := newMockPenaltyRepo()
	graph := newMockPenaltyGraph()
	graph.vouchers["v1"] = 1
	graph.vouchers["v2"] = 2
	graph.vouchers["v3"] = 3

	svc := NewModerationService(repo, graph, fixedClock)
	results, err := svc.PropagatePenalties(context.Background(), "action-1", "target-1", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 4 {
		t.Fatalf("got %d penalties, want 4", len(results))
	}

	// All should be permanent (DecaysAt == nil)
	for _, p := range results {
		if p.DecaysAt != nil {
			t.Errorf("penalty for %s has DecaysAt = %v, want nil (permanent)", p.UserID, p.DecaysAt)
		}
	}

	// Direct: 100.0
	if !approxEqual(results[0].PenaltyAmount, 100.0) {
		t.Errorf("direct penalty = %v, want 100.0", results[0].PenaltyAmount)
	}

	// Verify propagated amounts: 100 * 0.75^depth
	penaltyByUser := make(map[string]domain.TrustPenalty)
	for _, p := range results[1:] {
		penaltyByUser[p.UserID] = p
	}

	// Depth 1: 100 * 0.75 = 75.0
	if !approxEqual(penaltyByUser["v1"].PenaltyAmount, 75.0) {
		t.Errorf("v1 penalty = %v, want 75.0", penaltyByUser["v1"].PenaltyAmount)
	}
	// Depth 2: 100 * 0.75^2 = 56.25
	if !approxEqual(penaltyByUser["v2"].PenaltyAmount, 56.25) {
		t.Errorf("v2 penalty = %v, want 56.25", penaltyByUser["v2"].PenaltyAmount)
	}
	// Depth 3: 100 * 0.75^3 = 42.1875
	if !approxEqual(penaltyByUser["v3"].PenaltyAmount, 42.1875) {
		t.Errorf("v3 penalty = %v, want 42.1875", penaltyByUser["v3"].PenaltyAmount)
	}
}

func TestModerationService_PropagatePenalties_NoVouchers(t *testing.T) {
	repo := newMockPenaltyRepo()
	graph := newMockPenaltyGraph() // empty graph

	svc := NewModerationService(repo, graph, fixedClock)
	results, err := svc.PropagatePenalties(context.Background(), "action-1", "target-1", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("got %d penalties, want 1 (direct only)", len(results))
	}
	if results[0].UserID != "target-1" {
		t.Errorf("penalty.UserID = %q, want %q", results[0].UserID, "target-1")
	}
	if results[0].HopDepth != 0 {
		t.Errorf("penalty.HopDepth = %d, want 0", results[0].HopDepth)
	}
}

// --- Exact penalty amounts for all severity levels ---

func TestModerationService_PropagatePenalties_AllSeverityAmounts(t *testing.T) {
	tests := []struct {
		severity  int
		wantBase  float64
		wantDecay float64
		wantDays  int
	}{
		{1, 5.0, 0.50, 90},
		{2, 10.0, 0.70, 180},
		{3, 25.0, 0.60, 270},
		{4, 40.0, 0.70, 365},
		{5, 100.0, 0.75, 0},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("severity_%d", tt.severity), func(t *testing.T) {
			repo := newMockPenaltyRepo()
			graph := newMockPenaltyGraph()
			graph.vouchers["v1"] = 1

			svc := NewModerationService(repo, graph, fixedClock)
			results, err := svc.PropagatePenalties(context.Background(), "action-1", "target-1", tt.severity)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Check direct amount
			if !approxEqual(results[0].PenaltyAmount, tt.wantBase) {
				t.Errorf("direct penalty = %v, want %v", results[0].PenaltyAmount, tt.wantBase)
			}

			// Check propagated amount
			wantProp := tt.wantBase * tt.wantDecay
			penaltyByUser := make(map[string]domain.TrustPenalty)
			for _, p := range results[1:] {
				penaltyByUser[p.UserID] = p
			}
			if !approxEqual(penaltyByUser["v1"].PenaltyAmount, wantProp) {
				t.Errorf("propagated penalty = %v, want %v", penaltyByUser["v1"].PenaltyAmount, wantProp)
			}

			// Check decay timing
			if tt.wantDays == 0 {
				if results[0].DecaysAt != nil {
					t.Errorf("DecaysAt = %v, want nil (permanent)", results[0].DecaysAt)
				}
			} else {
				wantDecay := fixedNow.AddDate(0, 0, tt.wantDays)
				if results[0].DecaysAt == nil || !results[0].DecaysAt.Equal(wantDecay) {
					t.Errorf("DecaysAt = %v, want %v", results[0].DecaysAt, wantDecay)
				}
			}
		})
	}
}

// --- Error propagation ---

func TestModerationService_PropagatePenalties_GraphError(t *testing.T) {
	repo := newMockPenaltyRepo()
	graph := newMockPenaltyGraph()
	graph.err = errors.New("graph unavailable")

	svc := NewModerationService(repo, graph, fixedClock)
	results, err := svc.PropagatePenalties(context.Background(), "action-1", "target-1", 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Direct penalty should still be in results
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1 (direct penalty persisted before graph error)", len(results))
	}
	if results[0].UserID != "target-1" {
		t.Errorf("result[0].UserID = %q, want %q", results[0].UserID, "target-1")
	}

	// Verify direct penalty was persisted
	if len(repo.penalties) != 1 {
		t.Errorf("repo has %d penalties, want 1", len(repo.penalties))
	}
}

func TestModerationService_PropagatePenalties_DirectPenaltyRepoError(t *testing.T) {
	repo := newMockPenaltyRepo()
	repo.createErr = errors.New("db connection lost")

	svc := NewModerationService(repo, newMockPenaltyGraph(), fixedClock)
	_, err := svc.PropagatePenalties(context.Background(), "action-1", "target-1", 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestModerationService_PropagatePenalties_PropagatedPenaltyRepoError(t *testing.T) {
	repo := newMockPenaltyRepo()
	repo.failAfter = 1 // succeed on direct, fail on first propagated
	repo.createErr = errors.New("db write failed")
	graph := newMockPenaltyGraph()
	graph.vouchers["v1"] = 1

	svc := NewModerationService(repo, graph, fixedClock)
	results, err := svc.PropagatePenalties(context.Background(), "action-1", "target-1", 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Direct penalty should be in results
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
}

// --- State verification ---

func TestModerationService_PropagatePenalties_AllPenaltiesPersisted(t *testing.T) {
	repo := newMockPenaltyRepo()
	graph := newMockPenaltyGraph()
	graph.vouchers["v1"] = 1
	graph.vouchers["v2"] = 2

	svc := NewModerationService(repo, graph, fixedClock)
	_, err := svc.PropagatePenalties(context.Background(), "action-1", "target-1", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.penalties) != 3 {
		t.Errorf("repo has %d penalties, want 3 (1 direct + 2 propagated)", len(repo.penalties))
	}
}

func TestModerationService_PropagatePenalties_UniqueIDs(t *testing.T) {
	repo := newMockPenaltyRepo()
	graph := newMockPenaltyGraph()
	graph.vouchers["v1"] = 1
	graph.vouchers["v2"] = 2

	svc := NewModerationService(repo, graph, fixedClock)
	results, err := svc.PropagatePenalties(context.Background(), "action-1", "target-1", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	seen := make(map[string]bool)
	for _, p := range results {
		if p.ID == "" {
			t.Error("penalty has empty ID")
		}
		if seen[p.ID] {
			t.Errorf("duplicate penalty ID: %s", p.ID)
		}
		seen[p.ID] = true
	}
}

func TestModerationService_PropagatePenalties_ClockInjection(t *testing.T) {
	repo := newMockPenaltyRepo()
	graph := newMockPenaltyGraph()

	svc := NewModerationService(repo, graph, fixedClock)
	results, err := svc.PropagatePenalties(context.Background(), "action-1", "target-1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, p := range results {
		if !p.CreatedAt.Equal(fixedNow) {
			t.Errorf("penalty.CreatedAt = %v, want %v", p.CreatedAt, fixedNow)
		}
	}
}

func TestModerationService_PropagatePenalties_ActionIDPropagated(t *testing.T) {
	repo := newMockPenaltyRepo()
	graph := newMockPenaltyGraph()
	graph.vouchers["v1"] = 1

	svc := NewModerationService(repo, graph, fixedClock)
	results, err := svc.PropagatePenalties(context.Background(), "action-42", "target-1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, p := range results {
		if p.ModerationActionID != "action-42" {
			t.Errorf("penalty.ModerationActionID = %q, want %q", p.ModerationActionID, "action-42")
		}
	}
}

func TestNewModerationService_NilClock(t *testing.T) {
	svc := NewModerationService(newMockPenaltyRepo(), newMockPenaltyGraph(), nil)
	if svc.now == nil {
		t.Fatal("expected now to be set when nil clock passed")
	}
	// Verify it returns a reasonable time (not zero)
	now := svc.now()
	if now.IsZero() {
		t.Error("now() returned zero time")
	}
}


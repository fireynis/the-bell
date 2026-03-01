package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// --- mock for GetActionHistory tests ---

type mockActionHistoryRepo struct {
	actionsByTarget    []*domain.ModerationAction
	actionsByModerator []*domain.ModerationAction
	listErr            error
}

func newMockActionHistoryRepo() *mockActionHistoryRepo {
	return &mockActionHistoryRepo{}
}

func (m *mockActionHistoryRepo) CreateModerationAction(_ context.Context, _ *domain.ModerationAction) error {
	return nil
}

func (m *mockActionHistoryRepo) ListActionsByTarget(_ context.Context, _ string, _, _ int) ([]*domain.ModerationAction, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.actionsByTarget, nil
}

func (m *mockActionHistoryRepo) ListActionsByModerator(_ context.Context, _ string, _, _ int) ([]*domain.ModerationAction, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.actionsByModerator, nil
}

// --- mock PenaltyLister ---

type mockPenaltyListerS struct {
	penalties map[string][]domain.TrustPenalty
	listErr   error
}

func newMockPenaltyListerS() *mockPenaltyListerS {
	return &mockPenaltyListerS{penalties: make(map[string][]domain.TrustPenalty)}
}

func (m *mockPenaltyListerS) CreateTrustPenalty(_ context.Context, _ *domain.TrustPenalty) error {
	return nil
}

func (m *mockPenaltyListerS) ListPenaltiesByActionID(_ context.Context, actionID string) ([]domain.TrustPenalty, error) {
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
	return NewModerationActionService(actions, newMockActionUserLookup(), modSvc, nil, penaltyLister, fixedClock)
}

// --- GetActionHistory: by target ---

func TestModerationActionService_GetActionHistory_ByTarget(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	actionRepo := newMockActionHistoryRepo()
	actionRepo.actionsByTarget = []*domain.ModerationAction{
		{ID: "act-1", TargetUserID: "user-1", ModeratorID: "mod-1", Action: domain.ActionWarn, Severity: 1, Reason: "first", CreatedAt: now},
		{ID: "act-2", TargetUserID: "user-1", ModeratorID: "mod-1", Action: domain.ActionMute, Severity: 3, Reason: "second", CreatedAt: now},
	}

	penaltyLister := newMockPenaltyListerS()
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

	penaltyLister := newMockPenaltyListerS()
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
	penaltyLister := newMockPenaltyListerS()

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
	penaltyLister := newMockPenaltyListerS()

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

	penaltyLister := newMockPenaltyListerS()
	penaltyLister.listErr = errors.New("penalty db down")

	svc := newTestHistoryService(actionRepo, penaltyLister)
	_, err := svc.GetActionHistory(context.Background(), "user-1", false, 20, 0)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

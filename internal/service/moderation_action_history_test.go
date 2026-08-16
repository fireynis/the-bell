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

// Needed only to satisfy the full ModerationActionRepository when constructing
// a ModerationActionService for the delegation test; the history service itself
// takes the narrower ModerationActionLister.
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
	// calls counts round trips, which is what the batching is for: the history
	// pairs a whole page of actions with their penalties in one of these.
	calls int
}

func newMockPenaltyListerS() *mockPenaltyListerS {
	return &mockPenaltyListerS{penalties: make(map[string][]domain.TrustPenalty)}
}

func (m *mockPenaltyListerS) CreateTrustPenalty(_ context.Context, _ *domain.TrustPenalty) error {
	return nil
}

func (m *mockPenaltyListerS) ListPenaltiesByActionIDs(_ context.Context, actionIDs []string) ([]domain.TrustPenalty, error) {
	m.calls++
	if m.listErr != nil {
		return nil, m.listErr
	}

	// Returned as one flat set, in the id order asked for, exactly as the SQL
	// does — the service is what groups them back onto their actions.
	var out []domain.TrustPenalty
	for _, id := range actionIDs {
		out = append(out, m.penalties[id]...)
	}
	return out, nil
}

// --- helper ---

// Reading the audit trail needs the action log and the penalties, and nothing
// else: no user lookup, no clock, and no penalty engine standing on a vouch
// graph it never queries.
func newTestHistoryService(
	actions ModerationActionLister,
	penaltyLister PenaltyLister,
) *ModerationHistoryService {
	return NewModerationHistoryService(actions, penaltyLister)
}

// The shim on ModerationActionService is what the moderation handler and
// cmd/bell still call, so it has to keep returning the history service's
// answer until those are migrated across.
func TestModerationActionService_GetActionHistory_DelegatesToHistoryService(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	actionRepo := newMockActionHistoryRepo()
	actionRepo.actionsByTarget = []*domain.ModerationAction{
		{ID: "act-1", TargetUserID: "user-1", ModeratorID: "mod-1", Action: domain.ActionWarn, Severity: 1, Reason: "first", CreatedAt: now},
	}
	penaltyLister := newMockPenaltyListerS()
	penaltyLister.penalties["act-1"] = []domain.TrustPenalty{
		{ID: "pen-1", UserID: "user-1", ModerationActionID: "act-1", PenaltyAmount: 5.0, CreatedAt: now},
	}

	svc := NewModerationActionService(actionRepo, newMockActionUserLookup(), nil, nil, penaltyLister, nil, fixedClock)

	council := &domain.User{ID: "council-1", Role: domain.RoleCouncil, IsActive: true}
	entries, err := svc.GetActionHistory(context.Background(), council, "user-1", false, 20, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Action.ID != "act-1" {
		t.Errorf("action ID = %q, want %q", entries[0].Action.ID, "act-1")
	}
	if len(entries[0].Penalties) != 1 {
		t.Errorf("got %d penalties, want 1", len(entries[0].Penalties))
	}
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

// The penalties for a whole page are read in one query, not one per action.
// The page limit is 100, so the read this replaces was 101 round trips to
// render one screen — and the member's own history walks the same path.
func TestModerationHistoryService_GetActionHistory_ReadsPenaltiesInOneQuery(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	actionRepo := newMockActionHistoryRepo()
	penaltyLister := newMockPenaltyListerS()
	for _, id := range []string{"act-1", "act-2", "act-3"} {
		actionRepo.actionsByTarget = append(actionRepo.actionsByTarget, &domain.ModerationAction{
			ID: id, TargetUserID: "user-1", ModeratorID: "mod-1",
			Action: domain.ActionWarn, Severity: 1, Reason: "spam", CreatedAt: now,
		})
		penaltyLister.penalties[id] = []domain.TrustPenalty{
			{ID: "pen-" + id, UserID: "user-1", ModerationActionID: id, PenaltyAmount: 5.0, CreatedAt: now},
		}
	}

	entries, err := newTestHistoryService(actionRepo, penaltyLister).
		GetActionHistory(context.Background(), "user-1", false, 20, 0)
	if err != nil {
		t.Fatalf("GetActionHistory() unexpected error: %v", err)
	}

	if penaltyLister.calls != 1 {
		t.Errorf("penalty queries = %d, want 1 for %d actions", penaltyLister.calls, len(entries))
	}
	// Grouping the flat result set back onto the right action is the part the
	// batching could get wrong, so every entry's penalty must be its own.
	for _, entry := range entries {
		if len(entry.Penalties) != 1 {
			t.Fatalf("action %q has %d penalties, want 1", entry.Action.ID, len(entry.Penalties))
		}
		if got := entry.Penalties[0].ModerationActionID; got != entry.Action.ID {
			t.Errorf("action %q carries a penalty from %q", entry.Action.ID, got)
		}
	}
}

// The council-only rule on the by-moderator direction is the service's, not
// just the handler's: a route registered without the handler check must still
// not report which moderator handled which case.
func TestModerationActionService_GetActionHistory_ByModeratorRequiresCouncil(t *testing.T) {
	actionRepo := newMockActionHistoryRepo()
	actionRepo.actionsByModerator = []*domain.ModerationAction{
		{ID: "act-1", TargetUserID: "user-1", ModeratorID: "mod-1", Action: domain.ActionBan, Severity: 5, Reason: "banned"},
	}
	svc := NewModerationActionService(
		actionRepo, newMockActionUserLookup(), nil, nil, newMockPenaltyListerS(), nil, fixedClock)

	tests := []struct {
		name  string
		actor *domain.User
	}{
		{"nobody", nil},
		{"a member", &domain.User{ID: "u-1", Role: domain.RoleMember, IsActive: true}},
		{"a moderator", &domain.User{ID: "mod-9", Role: domain.RoleModerator, IsActive: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.GetActionHistory(context.Background(), tt.actor, "mod-1", true, 20, 0)
			if !errors.Is(err, ErrForbidden) {
				t.Errorf("GetActionHistory() error = %v, want ErrForbidden", err)
			}
		})
	}

	council := &domain.User{ID: "council-1", Role: domain.RoleCouncil, IsActive: true}
	entries, err := svc.GetActionHistory(context.Background(), council, "mod-1", true, 20, 0)
	if err != nil {
		t.Fatalf("council GetActionHistory() unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("council got %d entries, want 1", len(entries))
	}
}

// The audit trail names both parties, joined in by the query. The history
// service pairs each action with its penalties and must hand the action back
// whole; rebuilding it would drop the names and return the trail to two UUIDs
// per row.
func TestModerationHistoryService_GetActionHistory_CarriesDisplayNames(t *testing.T) {
	actions := newMockActionHistoryRepo()
	actions.actionsByTarget = []*domain.ModerationAction{{
		ID: "act-1", TargetUserID: "user-1", ModeratorID: "mod-1",
		Action: domain.ActionWarn, Severity: 1, Reason: "spam",
		TargetDisplayName: "Alice", ModeratorDisplayName: "Mallory",
	}}

	entries, err := NewModerationHistoryService(actions, newMockPenaltyListerS()).
		GetActionHistory(context.Background(), "user-1", false, 10, 0)
	if err != nil {
		t.Fatalf("GetActionHistory() unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d entries, want 1", len(entries))
	}
	got := entries[0].Action
	if got.TargetDisplayName != "Alice" || got.ModeratorDisplayName != "Mallory" {
		t.Errorf("names = %q/%q, want Alice/Mallory", got.TargetDisplayName, got.ModeratorDisplayName)
	}
}

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// --- mock ModerationActionRepository ---

type mockActionRepo struct {
	actions   []*domain.ModerationAction
	createErr error
}

func newMockActionRepo() *mockActionRepo {
	return &mockActionRepo{}
}

func (m *mockActionRepo) CreateModerationAction(_ context.Context, action *domain.ModerationAction) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.actions = append(m.actions, action)
	return nil
}

// --- mock ActionUserLookup ---

type mockActionUserLookup struct {
	users map[string]*domain.User
}

func newMockActionUserLookup() *mockActionUserLookup {
	return &mockActionUserLookup{users: make(map[string]*domain.User)}
}

func (m *mockActionUserLookup) GetUserByID(_ context.Context, id string) (*domain.User, error) {
	u, ok := m.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	return u, nil
}

// --- mock UserEnforcer ---

type mockUserEnforcer struct {
	deactivatedIDs []string
	roleUpdates    map[string]domain.Role
	trustUpdates   map[string]float64
	deactivateErr  error
	roleErr        error
	trustErr       error
}

func newMockUserEnforcer() *mockUserEnforcer {
	return &mockUserEnforcer{
		roleUpdates:  make(map[string]domain.Role),
		trustUpdates: make(map[string]float64),
	}
}

func (m *mockUserEnforcer) DeactivateUser(_ context.Context, id string) error {
	if m.deactivateErr != nil {
		return m.deactivateErr
	}
	m.deactivatedIDs = append(m.deactivatedIDs, id)
	return nil
}

func (m *mockUserEnforcer) UpdateUserRole(_ context.Context, id string, role domain.Role) error {
	if m.roleErr != nil {
		return m.roleErr
	}
	m.roleUpdates[id] = role
	return nil
}

func (m *mockUserEnforcer) UpdateUserTrustScore(_ context.Context, id string, score float64) error {
	if m.trustErr != nil {
		return m.trustErr
	}
	m.trustUpdates[id] = score
	return nil
}

// --- helpers ---

func newTestModerationActionService(
	actions ModerationActionRepository,
	users ActionUserLookup,
	penalties PenaltyRepository,
	graph PenaltyGraphQuerier,
	enforcer UserEnforcer,
) *ModerationActionService {
	modSvc := NewModerationService(penalties, graph, fixedClock)
	return NewModerationActionService(actions, users, modSvc, enforcer, fixedClock)
}

func int64Ptr(v int64) *int64 { return &v }

// --- Validation: action type ---

func TestModerationActionService_TakeAction_InvalidActionType(t *testing.T) {
	svc := newTestModerationActionService(
		newMockActionRepo(), newMockActionUserLookup(),
		newMockPenaltyRepo(), newMockPenaltyGraph(),
		nil,
	)
	_, err := svc.TakeAction(context.Background(), "mod-1", "user-1", "nuke", 1, "bad", nil)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want %v", err, ErrValidation)
	}
}

// --- Validation: severity / action type mismatch ---

func TestModerationActionService_TakeAction_SeverityMismatch(t *testing.T) {
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true}

	tests := []struct {
		name     string
		action   domain.ActionType
		severity int
	}{
		{"warn severity 3", domain.ActionWarn, 3},
		{"warn severity 4", domain.ActionWarn, 4},
		{"warn severity 5", domain.ActionWarn, 5},
		{"mute severity 1", domain.ActionMute, 1},
		{"mute severity 2", domain.ActionMute, 2},
		{"mute severity 4", domain.ActionMute, 4},
		{"suspend severity 1", domain.ActionSuspend, 1},
		{"suspend severity 3", domain.ActionSuspend, 3},
		{"ban severity 1", domain.ActionBan, 1},
		{"ban severity 4", domain.ActionBan, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestModerationActionService(
				newMockActionRepo(), users,
				newMockPenaltyRepo(), newMockPenaltyGraph(),
				nil,
			)
			_, err := svc.TakeAction(context.Background(), "mod-1", "target-1", tt.action, tt.severity, "reason", nil)
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("error = %v, want %v", err, ErrValidation)
			}
		})
	}
}

// --- Validation: severity out of range ---

func TestModerationActionService_TakeAction_SeverityOutOfRange(t *testing.T) {
	svc := newTestModerationActionService(
		newMockActionRepo(), newMockActionUserLookup(),
		newMockPenaltyRepo(), newMockPenaltyGraph(),
		nil,
	)
	_, err := svc.TakeAction(context.Background(), "mod-1", "user-1", domain.ActionWarn, 0, "reason", nil)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want %v", err, ErrValidation)
	}

	_, err = svc.TakeAction(context.Background(), "mod-1", "user-1", domain.ActionWarn, 6, "reason", nil)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want %v", err, ErrValidation)
	}
}

// --- Validation: empty reason ---

func TestModerationActionService_TakeAction_EmptyReason(t *testing.T) {
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true}

	svc := newTestModerationActionService(
		newMockActionRepo(), users,
		newMockPenaltyRepo(), newMockPenaltyGraph(),
		nil,
	)
	_, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionWarn, 1, "", nil)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want %v", err, ErrValidation)
	}
}

// --- Validation: reason too long ---

func TestModerationActionService_TakeAction_ReasonTooLong(t *testing.T) {
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true}

	longReason := make([]byte, maxActionReasonLen+1)
	for i := range longReason {
		longReason[i] = 'a'
	}

	svc := newTestModerationActionService(
		newMockActionRepo(), users,
		newMockPenaltyRepo(), newMockPenaltyGraph(),
		nil,
	)
	_, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionWarn, 1, string(longReason), nil)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want %v", err, ErrValidation)
	}
}

// --- Validation: self-moderation ---

func TestModerationActionService_TakeAction_SelfModeration(t *testing.T) {
	users := newMockActionUserLookup()
	users.users["mod-1"] = &domain.User{ID: "mod-1", IsActive: true, Role: domain.RoleModerator}

	svc := newTestModerationActionService(
		newMockActionRepo(), users,
		newMockPenaltyRepo(), newMockPenaltyGraph(),
		nil,
	)
	_, err := svc.TakeAction(context.Background(), "mod-1", "mod-1", domain.ActionWarn, 1, "reason", nil)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want %v", err, ErrValidation)
	}
}

// --- Validation: target not found ---

func TestModerationActionService_TakeAction_TargetNotFound(t *testing.T) {
	svc := newTestModerationActionService(
		newMockActionRepo(), newMockActionUserLookup(),
		newMockPenaltyRepo(), newMockPenaltyGraph(),
		nil,
	)
	_, err := svc.TakeAction(context.Background(), "mod-1", "nonexistent", domain.ActionWarn, 1, "reason", nil)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want %v", err, ErrNotFound)
	}
}

// --- Validation: ban with duration ---

func TestModerationActionService_TakeAction_BanWithDuration(t *testing.T) {
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true}

	svc := newTestModerationActionService(
		newMockActionRepo(), users,
		newMockPenaltyRepo(), newMockPenaltyGraph(),
		nil,
	)
	_, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionBan, 5, "reason", int64Ptr(3600))
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want %v", err, ErrValidation)
	}
}

// --- Validation: mute without duration ---

func TestModerationActionService_TakeAction_MuteWithoutDuration(t *testing.T) {
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true}

	svc := newTestModerationActionService(
		newMockActionRepo(), users,
		newMockPenaltyRepo(), newMockPenaltyGraph(),
		nil,
	)
	_, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionMute, 3, "reason", nil)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want %v", err, ErrValidation)
	}
}

// --- Validation: suspend without duration ---

func TestModerationActionService_TakeAction_SuspendWithoutDuration(t *testing.T) {
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true}

	svc := newTestModerationActionService(
		newMockActionRepo(), users,
		newMockPenaltyRepo(), newMockPenaltyGraph(),
		nil,
	)
	_, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionSuspend, 4, "reason", nil)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want %v", err, ErrValidation)
	}
}

// --- Success: valid warn ---

func TestModerationActionService_TakeAction_ValidWarn(t *testing.T) {
	actionRepo := newMockActionRepo()
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true}
	penaltyRepo := newMockPenaltyRepo()
	graph := newMockPenaltyGraph()

	svc := newTestModerationActionService(actionRepo, users, penaltyRepo, graph, nil)

	result, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionWarn, 1, "first warning", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Action == nil {
		t.Fatal("expected action, got nil")
	}
	if result.Action.ID == "" {
		t.Error("expected non-empty action ID")
	}
	if result.Action.TargetUserID != "target-1" {
		t.Errorf("target = %q, want %q", result.Action.TargetUserID, "target-1")
	}
	if result.Action.ModeratorID != "mod-1" {
		t.Errorf("moderator = %q, want %q", result.Action.ModeratorID, "mod-1")
	}
	if result.Action.Action != domain.ActionWarn {
		t.Errorf("action = %q, want %q", result.Action.Action, domain.ActionWarn)
	}
	if result.Action.Severity != 1 {
		t.Errorf("severity = %d, want 1", result.Action.Severity)
	}
	if result.Action.Reason != "first warning" {
		t.Errorf("reason = %q, want %q", result.Action.Reason, "first warning")
	}
	if result.Action.ExpiresAt != nil {
		t.Errorf("expires_at = %v, want nil for warn", result.Action.ExpiresAt)
	}
	if !result.Action.CreatedAt.Equal(fixedNow) {
		t.Errorf("created_at = %v, want %v", result.Action.CreatedAt, fixedNow)
	}

	// Verify action persisted
	if len(actionRepo.actions) != 1 {
		t.Errorf("persisted %d actions, want 1", len(actionRepo.actions))
	}

	// Verify penalties created (direct only, no vouchers)
	if len(result.Penalties) != 1 {
		t.Errorf("got %d penalties, want 1", len(result.Penalties))
	}
}

// --- Success: valid mute with duration ---

func TestModerationActionService_TakeAction_ValidMute(t *testing.T) {
	actionRepo := newMockActionRepo()
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true}
	penaltyRepo := newMockPenaltyRepo()
	graph := newMockPenaltyGraph()
	graph.vouchers["voucher-a"] = 1

	svc := newTestModerationActionService(actionRepo, users, penaltyRepo, graph, nil)

	dur := int64Ptr(3600) // 1 hour
	result, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionMute, 3, "muted for spam", dur)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Action.ExpiresAt == nil {
		t.Fatal("expected expires_at to be set for mute")
	}
	wantExpiry := fixedNow.Add(3600 * time.Second)
	if !result.Action.ExpiresAt.Equal(wantExpiry) {
		t.Errorf("expires_at = %v, want %v", result.Action.ExpiresAt, wantExpiry)
	}

	if result.Action.Duration == nil {
		t.Fatal("expected duration to be set")
	}
	if *result.Action.Duration != time.Hour {
		t.Errorf("duration = %v, want %v", *result.Action.Duration, time.Hour)
	}

	// 2 penalties: direct + 1 voucher
	if len(result.Penalties) != 2 {
		t.Errorf("got %d penalties, want 2", len(result.Penalties))
	}
}

// --- Success: valid ban ---

func TestModerationActionService_TakeAction_ValidBan(t *testing.T) {
	actionRepo := newMockActionRepo()
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true}
	penaltyRepo := newMockPenaltyRepo()
	graph := newMockPenaltyGraph()
	graph.vouchers["v1"] = 1
	graph.vouchers["v2"] = 2
	graph.vouchers["v3"] = 3

	svc := newTestModerationActionService(actionRepo, users, penaltyRepo, graph, nil)

	result, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionBan, 5, "banned permanently", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Action.ExpiresAt != nil {
		t.Errorf("expected nil expires_at for ban, got %v", result.Action.ExpiresAt)
	}

	// 4 penalties: direct + 3 vouchers
	if len(result.Penalties) != 4 {
		t.Errorf("got %d penalties, want 4", len(result.Penalties))
	}
}

// --- Success: all valid combos ---

func TestModerationActionService_TakeAction_AllValidCombos(t *testing.T) {
	tests := []struct {
		name     string
		action   domain.ActionType
		severity int
		duration *int64
	}{
		{"warn severity 1", domain.ActionWarn, 1, nil},
		{"warn severity 2", domain.ActionWarn, 2, nil},
		{"mute severity 3", domain.ActionMute, 3, int64Ptr(3600)},
		{"suspend severity 4", domain.ActionSuspend, 4, int64Ptr(86400)},
		{"ban severity 5", domain.ActionBan, 5, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users := newMockActionUserLookup()
			users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true}

			svc := newTestModerationActionService(
				newMockActionRepo(), users,
				newMockPenaltyRepo(), newMockPenaltyGraph(),
				nil,
			)

			result, err := svc.TakeAction(context.Background(), "mod-1", "target-1", tt.action, tt.severity, "valid reason", tt.duration)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Action.Action != tt.action {
				t.Errorf("action = %q, want %q", result.Action.Action, tt.action)
			}
			if result.Action.Severity != tt.severity {
				t.Errorf("severity = %d, want %d", result.Action.Severity, tt.severity)
			}
		})
	}
}

// --- Error: repo create fails ---

func TestModerationActionService_TakeAction_RepoError(t *testing.T) {
	actionRepo := newMockActionRepo()
	actionRepo.createErr = errors.New("db down")
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true}

	svc := newTestModerationActionService(
		actionRepo, users,
		newMockPenaltyRepo(), newMockPenaltyGraph(),
		nil,
	)

	_, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionWarn, 1, "reason", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- Warn with duration is allowed (duration ignored, no expires_at) ---

func TestModerationActionService_TakeAction_WarnWithDuration(t *testing.T) {
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true}

	svc := newTestModerationActionService(
		newMockActionRepo(), users,
		newMockPenaltyRepo(), newMockPenaltyGraph(),
		nil,
	)

	dur := int64Ptr(3600)
	result, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionWarn, 1, "warning", dur)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Warn with duration: duration/expires_at are set (we don't reject it)
	if result.Action.ExpiresAt == nil {
		t.Error("expected expires_at to be set when duration provided")
	}
}

// --- PropagatePenalties called with correct args ---

func TestModerationActionService_TakeAction_PenaltiesCalledCorrectly(t *testing.T) {
	actionRepo := newMockActionRepo()
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true}
	penaltyRepo := newMockPenaltyRepo()
	graph := newMockPenaltyGraph()
	graph.vouchers["v1"] = 1

	svc := newTestModerationActionService(actionRepo, users, penaltyRepo, graph, nil)

	result, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionWarn, 2, "moderate warning", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Direct penalty + 1 propagated
	if len(result.Penalties) != 2 {
		t.Fatalf("got %d penalties, want 2", len(result.Penalties))
	}

	// All penalties should reference the action ID
	for _, p := range result.Penalties {
		if p.ModerationActionID != result.Action.ID {
			t.Errorf("penalty.ModerationActionID = %q, want %q", p.ModerationActionID, result.Action.ID)
		}
	}

	// Direct penalty should target the target user
	if result.Penalties[0].UserID != "target-1" {
		t.Errorf("direct penalty user = %q, want %q", result.Penalties[0].UserID, "target-1")
	}

	// Direct penalty amount for severity 2 is 10.0
	if !approxEqual(result.Penalties[0].PenaltyAmount, 10.0) {
		t.Errorf("direct penalty = %v, want 10.0", result.Penalties[0].PenaltyAmount)
	}
}

// --- Enforcement: mute drops trust below posting threshold ---

func TestModerationActionService_TakeAction_MuteEnforcement_HighTrust(t *testing.T) {
	actionRepo := newMockActionRepo()
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, TrustScore: 80.0}
	enforcer := newMockUserEnforcer()

	svc := newTestModerationActionService(actionRepo, users, newMockPenaltyRepo(), newMockPenaltyGraph(), enforcer)

	_, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionMute, 3, "muted", int64Ptr(3600))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	score, ok := enforcer.trustUpdates["target-1"]
	if !ok {
		t.Fatal("expected trust score update for target-1")
	}
	if score != 29.0 {
		t.Errorf("trust score = %v, want 29.0", score)
	}
}

// --- Enforcement: mute no-op when trust already below threshold ---

func TestModerationActionService_TakeAction_MuteEnforcement_LowTrust(t *testing.T) {
	actionRepo := newMockActionRepo()
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, TrustScore: 20.0}
	enforcer := newMockUserEnforcer()

	svc := newTestModerationActionService(actionRepo, users, newMockPenaltyRepo(), newMockPenaltyGraph(), enforcer)

	_, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionMute, 3, "already low trust", int64Ptr(3600))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := enforcer.trustUpdates["target-1"]; ok {
		t.Error("expected no trust update when trust already below threshold")
	}
}

// --- Enforcement: suspend deactivates user ---

func TestModerationActionService_TakeAction_SuspendEnforcement(t *testing.T) {
	actionRepo := newMockActionRepo()
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, TrustScore: 50.0}
	enforcer := newMockUserEnforcer()

	svc := newTestModerationActionService(actionRepo, users, newMockPenaltyRepo(), newMockPenaltyGraph(), enforcer)

	_, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionSuspend, 4, "suspended", int64Ptr(86400))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(enforcer.deactivatedIDs) != 1 || enforcer.deactivatedIDs[0] != "target-1" {
		t.Errorf("deactivated = %v, want [target-1]", enforcer.deactivatedIDs)
	}
}

// --- Enforcement: ban sets role=banned and trust=0 ---

func TestModerationActionService_TakeAction_BanEnforcement(t *testing.T) {
	actionRepo := newMockActionRepo()
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, TrustScore: 50.0, Role: domain.RoleMember}
	enforcer := newMockUserEnforcer()

	svc := newTestModerationActionService(actionRepo, users, newMockPenaltyRepo(), newMockPenaltyGraph(), enforcer)

	_, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionBan, 5, "banned", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	role, ok := enforcer.roleUpdates["target-1"]
	if !ok {
		t.Fatal("expected role update for target-1")
	}
	if role != domain.RoleBanned {
		t.Errorf("role = %q, want %q", role, domain.RoleBanned)
	}

	score, ok := enforcer.trustUpdates["target-1"]
	if !ok {
		t.Fatal("expected trust update for target-1")
	}
	if score != 0 {
		t.Errorf("trust = %v, want 0", score)
	}
}

// --- Enforcement: warn does not enforce ---

func TestModerationActionService_TakeAction_WarnNoEnforcement(t *testing.T) {
	actionRepo := newMockActionRepo()
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, TrustScore: 50.0}
	enforcer := newMockUserEnforcer()

	svc := newTestModerationActionService(actionRepo, users, newMockPenaltyRepo(), newMockPenaltyGraph(), enforcer)

	_, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionWarn, 1, "warning", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(enforcer.deactivatedIDs) != 0 {
		t.Error("expected no deactivation for warn")
	}
	if len(enforcer.roleUpdates) != 0 {
		t.Error("expected no role update for warn")
	}
	if len(enforcer.trustUpdates) != 0 {
		t.Error("expected no trust update for warn")
	}
}

// --- Enforcement: failure returns partial result ---

func TestModerationActionService_TakeAction_EnforcementError(t *testing.T) {
	actionRepo := newMockActionRepo()
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, TrustScore: 50.0}
	enforcer := newMockUserEnforcer()
	enforcer.deactivateErr = errors.New("db unavailable")

	svc := newTestModerationActionService(actionRepo, users, newMockPenaltyRepo(), newMockPenaltyGraph(), enforcer)

	result, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionSuspend, 4, "suspended", int64Ptr(86400))
	if err == nil {
		t.Fatal("expected error from enforcement failure")
	}
	// Action should still be created and returned (partial success)
	if result == nil || result.Action == nil {
		t.Fatal("expected action to be returned despite enforcement failure")
	}
}

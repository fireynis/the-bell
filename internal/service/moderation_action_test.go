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
	actions            []*domain.ModerationAction
	actionsByTarget    []*domain.ModerationAction
	actionsByModerator []*domain.ModerationAction
	createErr          error
	listErr            error
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

// --- mock UserEnforcer ---

type mockUserEnforcer struct {
	deactivatedIDs []string
	roleUpdates    map[string]domain.Role
	trustUpdates   map[string]float64
	// mutes records every SetUserMutedUntil call, including the nil that lifts
	// a mute, so a test can tell "muted until nil" from "never called".
	mutes         map[string]*time.Time
	mutedIDs      []string
	deactivateErr error
	roleErr       error
	trustErr      error
	muteErr       error
}

func newMockUserEnforcer() *mockUserEnforcer {
	return &mockUserEnforcer{
		roleUpdates:  make(map[string]domain.Role),
		trustUpdates: make(map[string]float64),
		mutes:        make(map[string]*time.Time),
	}
}

func (m *mockUserEnforcer) SetUserMutedUntil(_ context.Context, id string, until *time.Time) error {
	if m.muteErr != nil {
		return m.muteErr
	}
	m.mutes[id] = until
	m.mutedIDs = append(m.mutedIDs, id)
	return nil
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
	return NewModerationActionService(actions, users, modSvc, enforcer, nil, fixedClock)
}

func int64Ptr(v int64) *int64 { return &v }

// --- Validation: orchestration ---

// The individual validation rules are tested directly against
// validateActionRequest in moderation_action_policy_test.go. What belongs here
// is the wiring: a rejected request must abandon the whole operation before it
// touches anything. A validation error that still wrote an action, looked the
// target up, or enforced a penalty would leave the audit log describing
// moderation that never legitimately happened.
func TestModerationActionService_TakeAction_ValidationFailureTouchesNothing(t *testing.T) {
	// One representative rejection per rule; exhaustive coverage is the pure
	// function's job.
	tests := []struct {
		name     string
		action   domain.ActionType
		severity int
		reason   string
		duration *int64
		target   string
	}{
		{"unknown action type", domain.ActionType("nuke"), 1, "reason", nil, "target-1"},
		{"severity not valid for action", domain.ActionWarn, 5, "reason", nil, "target-1"},
		{"severity out of range", domain.ActionWarn, 99, "reason", nil, "target-1"},
		{"empty reason", domain.ActionWarn, 1, "", nil, "target-1"},
		{"self-moderation", domain.ActionWarn, 1, "reason", nil, "mod-1"},
		{"ban with a duration", domain.ActionBan, 5, "reason", int64Ptr(3600), "target-1"},
		{"mute without a duration", domain.ActionMute, 3, "reason", nil, "target-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actions := newMockActionRepo()
			users := newFakeUserStore()
			users.add(&domain.User{ID: "target-1", IsActive: true})
			users.add(&domain.User{ID: "mod-1", IsActive: true, Role: domain.RoleModerator})
			enforcer := newMockUserEnforcer()

			svc := newTestModerationActionService(
				actions, users, newMockPenaltyRepo(), newMockPenaltyGraph(), enforcer,
			)

			_, err := svc.TakeAction(context.Background(), "mod-1", tt.target, tt.action, tt.severity, tt.reason, tt.duration)

			if !errors.Is(err, ErrValidation) {
				t.Fatalf("error = %v, want %v", err, ErrValidation)
			}
			if len(actions.actions) != 0 {
				t.Errorf("%d moderation actions were written despite the request being rejected", len(actions.actions))
			}
			if len(enforcer.deactivatedIDs) != 0 || len(enforcer.roleUpdates) != 0 || len(enforcer.trustUpdates) != 0 {
				t.Error("enforcement ran against the target despite the request being rejected")
			}
		})
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

// --- Enforcement: mute records its expiry ---

// The mute length is the one the moderator chose, and it is the same instant
// the action row records. Deriving it twice would let the two drift.
func TestModerationActionService_TakeAction_MuteSetsMutedUntilFromTheChosenDuration(t *testing.T) {
	actionRepo := newMockActionRepo()
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, TrustScore: 80.0}
	enforcer := newMockUserEnforcer()

	svc := newTestModerationActionService(actionRepo, users, newMockPenaltyRepo(), newMockPenaltyGraph(), enforcer)

	result, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionMute, 3, "muted", int64Ptr(3600))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	until, ok := enforcer.mutes["target-1"]
	if !ok {
		t.Fatal("expected muted_until to be set for target-1")
	}
	if until == nil {
		t.Fatal("muted_until was set to nil, which lifts the mute")
	}
	if want := fixedNow.Add(time.Hour); !until.Equal(want) {
		t.Errorf("muted_until = %v, want %v — the hour the moderator chose", until, want)
	}
	// The action row and the user column must name the same instant.
	if result.Action.ExpiresAt == nil || !result.Action.ExpiresAt.Equal(*until) {
		t.Errorf("action expires_at = %v but muted_until = %v; they must agree", result.Action.ExpiresAt, until)
	}
}

// A mute must not move the trust score. Dropping it below the posting threshold
// was the old mechanism, and it both stacked a second punishment on the penalty
// the action already propagates and was undone by the next recalculation.
func TestModerationActionService_TakeAction_MuteLeavesTrustAlone(t *testing.T) {
	actionRepo := newMockActionRepo()
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, TrustScore: 80.0}
	enforcer := newMockUserEnforcer()

	svc := newTestModerationActionService(actionRepo, users, newMockPenaltyRepo(), newMockPenaltyGraph(), enforcer)

	if _, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionMute, 3, "muted", int64Ptr(3600)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if score, ok := enforcer.trustUpdates["target-1"]; ok {
		t.Errorf("mute wrote trust score %v; muted_until is the mechanism now", score)
	}
	if len(enforcer.deactivatedIDs) != 0 {
		t.Errorf("mute deactivated %v; a mute is not a suspension", enforcer.deactivatedIDs)
	}
	if len(enforcer.roleUpdates) != 0 {
		t.Errorf("mute changed roles %v; a mute is not a ban", enforcer.roleUpdates)
	}
}

// --- Enforcement: mute no-op when trust already below threshold ---

// A user already below the posting threshold is still muted. The old
// trust-drop mechanism skipped them — they could not post anyway — but that
// left no record of the mute, so it lapsed silently the moment their score
// recovered.
func TestModerationActionService_TakeAction_MuteAppliesBelowThePostingThreshold(t *testing.T) {
	actionRepo := newMockActionRepo()
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, TrustScore: 20.0}
	enforcer := newMockUserEnforcer()

	svc := newTestModerationActionService(actionRepo, users, newMockPenaltyRepo(), newMockPenaltyGraph(), enforcer)

	_, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionMute, 3, "already low trust", int64Ptr(3600))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	until, ok := enforcer.mutes["target-1"]
	if !ok {
		t.Fatal("a low-trust user was not muted; their score may recover before the mute would have")
	}
	if until == nil || !until.Equal(fixedNow.Add(time.Hour)) {
		t.Errorf("muted_until = %v, want %v", until, fixedNow.Add(time.Hour))
	}
	if _, ok := enforcer.trustUpdates["target-1"]; ok {
		t.Error("a mute wrote a trust score")
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

// --- PenaltyPropagator decoupling ---

// stubPropagator records the propagation request and returns a canned result.
// Its existence is the point of the PenaltyPropagator interface: taking an
// action can now be tested without standing up a penalty repository and a
// vouch graph behind a concrete *ModerationService.
type stubPropagator struct {
	actionID     string
	targetUserID string
	severity     int
	calls        int
	result       []domain.TrustPenalty
	err          error
}

func (s *stubPropagator) PropagatePenalties(_ context.Context, actionID, targetUserID string, severity int) ([]domain.TrustPenalty, error) {
	s.calls++
	s.actionID, s.targetUserID, s.severity = actionID, targetUserID, severity
	return s.result, s.err
}

func TestModerationActionService_TakeAction_PropagatesThroughTheInterface(t *testing.T) {
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, TrustScore: 50.0}
	prop := &stubPropagator{result: []domain.TrustPenalty{{ID: "pen-1", UserID: "target-1"}}}

	svc := NewModerationActionService(newMockActionRepo(), users, prop, newMockUserEnforcer(), nil, fixedClock)

	result, err := svc.TakeAction(context.Background(), "mod-1", "target-1", domain.ActionWarn, 2, "reason", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prop.calls != 1 {
		t.Fatalf("propagator called %d times, want 1", prop.calls)
	}
	if prop.actionID != result.Action.ID {
		t.Errorf("propagated action ID = %q, want %q", prop.actionID, result.Action.ID)
	}
	if prop.targetUserID != "target-1" {
		t.Errorf("propagated target = %q, want %q", prop.targetUserID, "target-1")
	}
	if prop.severity != 2 {
		t.Errorf("propagated severity = %d, want 2", prop.severity)
	}
	if len(result.Penalties) != 1 || result.Penalties[0].ID != "pen-1" {
		t.Errorf("penalties = %+v, want the propagator's result", result.Penalties)
	}
}

// --- Enforcement survives an unreachable vouch graph ---

// Enforcement depends only on the action type and the user, never on the vouch
// graph. When propagation ran first and returned early on a graph error, a ban
// left the user un-banned with their trust intact — still able to post.
func TestModerationActionService_TakeAction_EnforcesWhenGraphUnavailable(t *testing.T) {
	graphErr := errors.New("AGE unavailable")

	tests := []struct {
		name     string
		action   domain.ActionType
		severity int
		duration *int64
		verify   func(t *testing.T, e *mockUserEnforcer)
	}{
		{
			name: "mute records its expiry", action: domain.ActionMute, severity: 3, duration: int64Ptr(3600),
			verify: func(t *testing.T, e *mockUserEnforcer) {
				until, ok := e.mutes["target-1"]
				if !ok {
					t.Fatal("expected muted_until to be set for target-1")
				}
				if until == nil || !until.Equal(fixedNow.Add(time.Hour)) {
					t.Errorf("muted_until = %v, want %v", until, fixedNow.Add(time.Hour))
				}
			},
		},
		{
			name: "suspend deactivates", action: domain.ActionSuspend, severity: 4, duration: int64Ptr(86400),
			verify: func(t *testing.T, e *mockUserEnforcer) {
				if len(e.deactivatedIDs) != 1 || e.deactivatedIDs[0] != "target-1" {
					t.Errorf("deactivated = %v, want [target-1]", e.deactivatedIDs)
				}
			},
		},
		{
			name: "ban sets role and zeroes trust", action: domain.ActionBan, severity: 5,
			verify: func(t *testing.T, e *mockUserEnforcer) {
				if role := e.roleUpdates["target-1"]; role != domain.RoleBanned {
					t.Errorf("role = %q, want %q", role, domain.RoleBanned)
				}
				if score, ok := e.trustUpdates["target-1"]; !ok || score != 0 {
					t.Errorf("trust = %v (set=%t), want 0", score, ok)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users := newMockActionUserLookup()
			users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, TrustScore: 80.0, Role: domain.RoleMember}
			graph := newMockPenaltyGraph()
			graph.err = graphErr
			enforcer := newMockUserEnforcer()

			svc := newTestModerationActionService(newMockActionRepo(), users, newMockPenaltyRepo(), graph, enforcer)

			result, err := svc.TakeAction(context.Background(), "mod-1", "target-1", tt.action, tt.severity, "reason", tt.duration)

			// Partial-success contract: the action was persisted, so it is
			// returned alongside the propagation failure.
			if !errors.Is(err, graphErr) {
				t.Fatalf("error = %v, want it to wrap %v", err, graphErr)
			}
			if result == nil || result.Action == nil {
				t.Fatal("expected action to be returned despite propagation failure")
			}

			tt.verify(t, enforcer)
		})
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

package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

func secs(n int64) *int64 { return &n }

func TestValidateActionRequest_Valid(t *testing.T) {
	tests := []struct {
		name       string
		actionType domain.ActionType
		severity   int
		duration   *int64
		wantDur    *time.Duration
	}{
		{"warn at severity 1", domain.ActionWarn, 1, nil, nil},
		{"warn at severity 2", domain.ActionWarn, 2, nil, nil},
		{"mute with duration", domain.ActionMute, 3, secs(3600), durPtr(time.Hour)},
		{"suspend with duration", domain.ActionSuspend, 4, secs(86400), durPtr(24 * time.Hour)},
		{"ban without duration", domain.ActionBan, 5, nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateActionRequest("mod", "target", tt.actionType, tt.severity, "  spam  ", tt.duration)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.ActionType != tt.actionType || got.Severity != tt.severity {
				t.Errorf("got %+v, want action %q severity %d", got, tt.actionType, tt.severity)
			}
			if got.Reason != "spam" {
				t.Errorf("Reason = %q, want the trimmed %q", got.Reason, "spam")
			}
			switch {
			case tt.wantDur == nil && got.Duration != nil:
				t.Errorf("Duration = %v, want nil", *got.Duration)
			case tt.wantDur != nil && got.Duration == nil:
				t.Errorf("Duration = nil, want %v", *tt.wantDur)
			case tt.wantDur != nil && *got.Duration != *tt.wantDur:
				t.Errorf("Duration = %v, want %v", *got.Duration, *tt.wantDur)
			}
		})
	}
}

func durPtr(d time.Duration) *time.Duration { return &d }

func TestValidateActionRequest_Rejects(t *testing.T) {
	tests := []struct {
		name       string
		moderator  string
		target     string
		actionType domain.ActionType
		severity   int
		reason     string
		duration   *int64
		wantMsg    string
	}{
		{
			name: "unknown action type", moderator: "mod", target: "t",
			actionType: domain.ActionType("shadowban"), severity: 1, reason: "x",
			wantMsg: "invalid action type",
		},
		{
			name: "ban severity on a warn", moderator: "mod", target: "t",
			actionType: domain.ActionWarn, severity: 5, reason: "x",
			wantMsg: "not valid for action type",
		},
		{
			name: "mute at warn severity", moderator: "mod", target: "t",
			actionType: domain.ActionMute, severity: 1, reason: "x", duration: secs(60),
			wantMsg: "not valid for action type",
		},
		{
			name: "severity below the range", moderator: "mod", target: "t",
			actionType: domain.ActionWarn, severity: 0, reason: "x",
			wantMsg: "not valid for action type",
		},
		{
			name: "severity above the range", moderator: "mod", target: "t",
			actionType: domain.ActionBan, severity: 6, reason: "x",
			wantMsg: "not valid for action type",
		},
		{
			name: "empty reason", moderator: "mod", target: "t",
			actionType: domain.ActionWarn, severity: 1, reason: "",
			wantMsg: "reason must not be empty",
		},
		{
			name: "whitespace-only reason", moderator: "mod", target: "t",
			actionType: domain.ActionWarn, severity: 1, reason: "   \t\n ",
			wantMsg: "reason must not be empty",
		},
		{
			name: "reason too long", moderator: "mod", target: "t",
			actionType: domain.ActionWarn, severity: 1, reason: strings.Repeat("a", maxActionReasonLen+1),
			wantMsg: "exceeds",
		},
		{
			name: "self-moderation", moderator: "same", target: "same",
			actionType: domain.ActionWarn, severity: 1, reason: "x",
			wantMsg: "cannot moderate yourself",
		},
		{
			name: "ban with a duration", moderator: "mod", target: "t",
			actionType: domain.ActionBan, severity: 5, reason: "x", duration: secs(60),
			wantMsg: "bans cannot have a duration",
		},
		{
			name: "warn with a duration", moderator: "mod", target: "t",
			actionType: domain.ActionWarn, severity: 1, reason: "x", duration: secs(60),
			wantMsg: "warnings cannot have a duration",
		},
		{
			name: "mute without a duration", moderator: "mod", target: "t",
			actionType: domain.ActionMute, severity: 3, reason: "x",
			wantMsg: "requires a duration",
		},
		{
			name: "suspend without a duration", moderator: "mod", target: "t",
			actionType: domain.ActionSuspend, severity: 4, reason: "x",
			wantMsg: "requires a duration",
		},
		{
			name: "zero duration", moderator: "mod", target: "t",
			actionType: domain.ActionMute, severity: 3, reason: "x", duration: secs(0),
			wantMsg: "duration must be positive",
		},
		{
			name: "negative duration", moderator: "mod", target: "t",
			actionType: domain.ActionMute, severity: 3, reason: "x", duration: secs(-60),
			wantMsg: "duration must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateActionRequest(tt.moderator, tt.target, tt.actionType, tt.severity, tt.reason, tt.duration)
			if err == nil {
				t.Fatal("expected a validation error, got nil")
			}
			if !errors.Is(err, ErrValidation) {
				t.Errorf("error %v does not wrap ErrValidation", err)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to mention %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

// Every action type must accept exactly the severities that map to its trust
// penalty; a mismatch would let a moderator pick an arbitrary penalty size.
func TestValidateActionRequest_SeverityIsBoundToActionType(t *testing.T) {
	needsDuration := map[domain.ActionType]*int64{
		domain.ActionMute:    secs(60),
		domain.ActionSuspend: secs(60),
	}

	for actionType, allowed := range allowedSeverity {
		for severity := 1; severity <= 5; severity++ {
			_, err := validateActionRequest("mod", "t", actionType, severity, "reason", needsDuration[actionType])

			shouldPass := false
			for _, a := range allowed {
				if a == severity {
					shouldPass = true
				}
			}

			if shouldPass && err != nil {
				t.Errorf("%s severity %d: unexpected error %v", actionType, severity, err)
			}
			if !shouldPass && err == nil {
				t.Errorf("%s severity %d: expected rejection, got none", actionType, severity)
			}
		}
	}
}

func TestPlanEnforcement(t *testing.T) {
	tests := []struct {
		name       string
		actionType domain.ActionType
		want       []enforcementStep
	}{
		{"warn changes no state", domain.ActionWarn, nil},
		{"mute records its expiry", domain.ActionMute, []enforcementStep{enforceMute}},
		{"suspend records its expiry", domain.ActionSuspend, []enforcementStep{enforceSuspend}},
		{"ban sets role and zeroes trust", domain.ActionBan, []enforcementStep{enforceBanRole, enforceZeroTrust}},
		{"unknown action changes no state", domain.ActionType("???"), nil},
		{"empty action changes no state", domain.ActionType(""), nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := planEnforcement(tt.actionType)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("step %d = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// A mute writes muted_until and nothing else. It must not touch the trust
// score: it used to drop the score below the posting threshold, which the trust
// worker recomputed away within seconds, and which also stacked a second
// punishment on top of the penalty the action already propagates.
func TestPlanEnforcement_MuteDoesNotTouchTrust(t *testing.T) {
	for _, step := range planEnforcement(domain.ActionMute) {
		if step == enforceZeroTrust {
			t.Error("mute plan zeroes trust; a mute is not a ban")
		}
		if step != enforceMute {
			t.Errorf("mute plan contains %v, want only enforceMute", step)
		}
	}
}

// The mute applies regardless of the user's current score. Keying it on the
// score — as the old trust-drop version had to — meant a user already below the
// threshold was never actually muted, so their mute silently lapsed the moment
// their score recovered.
func TestPlanEnforcement_MuteIsIndependentOfTrustScore(t *testing.T) {
	want := planEnforcement(domain.ActionMute)

	for _, score := range []float64{0, domain.PostingThreshold - 10, domain.PostingThreshold, 100} {
		got := planEnforcement(domain.ActionMute)
		if len(got) != len(want) || len(got) != 1 || got[0] != enforceMute {
			t.Errorf("score %v: plan = %v, want a mute regardless of score", score, got)
		}
	}
}

// A ban must revoke posting rights via the role and clear the score; doing only
// one would leave a banned account able to recover trust or keep its old score.
func TestPlanEnforcement_BanIsBothRoleAndTrust(t *testing.T) {
	steps := planEnforcement(domain.ActionBan)

	var sawRole, sawTrust bool
	for _, s := range steps {
		switch s {
		case enforceBanRole:
			sawRole = true
		case enforceZeroTrust:
			sawTrust = true
		}
	}
	if !sawRole || !sawTrust {
		t.Errorf("ban plan = %v, want both the banned role and a zeroed trust score", steps)
	}
}

// --- Lifting a mute ---

// The route group already carries a moderator guard, so these cases are the
// ones that reach the service anyway: a caller who arrived by another route, a
// moderator whose role changed mid-session, and — the one no route guard can
// see — a moderator lifting the mute a colleague placed on them.
func TestCanLiftMute(t *testing.T) {
	tests := []struct {
		name      string
		moderator *domain.User
		target    string
		wantErr   error
	}{
		{"moderator lifts another user's mute", &domain.User{ID: "mod-1", Role: domain.RoleModerator, IsActive: true}, "target-1", nil},
		{"council lifts another user's mute", &domain.User{ID: "c-1", Role: domain.RoleCouncil, IsActive: true}, "target-1", nil},
		{"no caller at all", nil, "target-1", ErrForbidden},
		{"member", &domain.User{ID: "u-1", Role: domain.RoleMember, IsActive: true}, "target-1", ErrForbidden},
		{"pending", &domain.User{ID: "u-1", Role: domain.RolePending, IsActive: true}, "target-1", ErrForbidden},
		{"banned", &domain.User{ID: "u-1", Role: domain.RoleBanned, IsActive: true}, "target-1", ErrForbidden},
		{"moderator lifting their own mute", &domain.User{ID: "mod-1", Role: domain.RoleModerator, IsActive: true}, "mod-1", ErrValidation},
		{"council lifting their own mute", &domain.User{ID: "c-1", Role: domain.RoleCouncil, IsActive: true}, "c-1", ErrValidation},
		{"empty target", &domain.User{ID: "mod-1", Role: domain.RoleModerator, IsActive: true}, "", ErrValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := canLiftMute(tt.moderator, tt.target)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// Self-moderation is refused before the role is considered, matching
// validateActionRequest's ordering: a muted member who somehow reaches this is
// told the specific thing that is wrong rather than being sent to acquire a
// role that still would not let them do it.
func TestCanLiftMute_SelfIsRefusedEvenWithoutTheRole(t *testing.T) {
	member := &domain.User{ID: "u-1", Role: domain.RoleMember, IsActive: true}

	err := canLiftMute(member, member.ID)

	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want %v", err, ErrValidation)
	}
	if !strings.Contains(err.Error(), "yourself") {
		t.Errorf("error = %q, want it to name self-moderation", err)
	}
}

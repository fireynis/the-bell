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
	above := &domain.User{ID: "u", TrustScore: domain.PostingThreshold + 10}
	below := &domain.User{ID: "u", TrustScore: domain.PostingThreshold - 10}

	tests := []struct {
		name       string
		actionType domain.ActionType
		user       *domain.User
		want       []enforcementStep
	}{
		{"warn changes no state", domain.ActionWarn, above, nil},
		{"mute silences a user who can post", domain.ActionMute, above, []enforcementStep{enforceDropBelowPostingThreshold}},
		{"mute is a no-op below the threshold", domain.ActionMute, below, nil},
		{"mute at exactly the threshold still acts", domain.ActionMute, &domain.User{TrustScore: domain.PostingThreshold}, []enforcementStep{enforceDropBelowPostingThreshold}},
		{"suspend deactivates", domain.ActionSuspend, above, []enforcementStep{enforceDeactivate}},
		{"ban sets role and zeroes trust", domain.ActionBan, above, []enforcementStep{enforceBanRole, enforceZeroTrust}},
		{"unknown action changes no state", domain.ActionType("???"), above, nil},
		{"nil user does not panic", domain.ActionMute, nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := planEnforcement(tt.actionType, tt.user)
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

// A ban must revoke posting rights via the role and clear the score; doing only
// one would leave a banned account able to recover trust or keep its old score.
func TestPlanEnforcement_BanIsBothRoleAndTrust(t *testing.T) {
	steps := planEnforcement(domain.ActionBan, &domain.User{TrustScore: 95})

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

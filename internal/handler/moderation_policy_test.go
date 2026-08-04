package handler

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/service"
)

// Listing the actions taken BY a moderator reveals which moderator handled
// which case. That is an audit view for the council, not something a moderator
// or an ordinary member may read.
func TestCanQueryByModerator(t *testing.T) {
	tests := []struct {
		name string
		user *domain.User
		want bool
	}{
		{"council", &domain.User{Role: domain.RoleCouncil, IsActive: true}, true},
		{"moderator cannot audit other moderators", &domain.User{Role: domain.RoleModerator, IsActive: true}, false},
		{"member", &domain.User{Role: domain.RoleMember, IsActive: true}, false},
		{"pending", &domain.User{Role: domain.RolePending, IsActive: true}, false},
		{"banned", &domain.User{Role: domain.RoleBanned, IsActive: true}, false},
		{"deactivated council member loses the view", &domain.User{Role: domain.RoleCouncil, IsActive: false}, false},
		{"no user", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canQueryByModerator(tt.user); got != tt.want {
				t.Errorf("canQueryByModerator() = %v, want %v", got, tt.want)
			}
		})
	}
}

func actionResult() *service.TakeActionResult {
	return &service.TakeActionResult{
		Action: &domain.ModerationAction{ID: "action-1", Action: domain.ActionWarn},
	}
}

func TestTakeActionOutcome(t *testing.T) {
	tests := []struct {
		name        string
		result      *service.TakeActionResult
		err         error
		wantStatus  int
		wantPartial bool
	}{
		{"action created", actionResult(), nil, http.StatusCreated, false},
		{"nothing created, target not found", nil, service.ErrNotFound, http.StatusNotFound, false},
		{"nothing created, request rejected", nil, service.ErrValidation, http.StatusBadRequest, false},
		{"nothing created, caller not allowed", nil, service.ErrForbidden, http.StatusForbidden, false},
		{"nothing created, unknown failure", nil, errors.New("boom"), http.StatusInternalServerError, false},
		{"result without an action is not a creation", &service.TakeActionResult{}, service.ErrNotFound, http.StatusNotFound, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, partial := takeActionOutcome(tt.result, tt.err)
			if status != tt.wantStatus {
				t.Errorf("status = %d, want %d", status, tt.wantStatus)
			}
			if partial != tt.wantPartial {
				t.Errorf("partial = %v, want %v", partial, tt.wantPartial)
			}
		})
	}
}

// The subtle case: TakeAction persists the moderation action before it
// propagates trust penalties, so a propagation (or enforcement) failure comes
// back as an error alongside a real action. That action exists and cannot be
// taken back — reporting a failure would tell the moderator to retry and
// duplicate it. It stays a 201, and the failure is only logged.
func TestTakeActionOutcome_CreatedActionSurvivesPenaltyFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"penalty propagation failed", fmt.Errorf("propagating penalties: %w", errors.New("age query failed"))},
		{"enforcement failed", fmt.Errorf("enforcing action: %w", errors.New("deactivating user"))},
		{"failure wraps a mapped sentinel", fmt.Errorf("propagating penalties: %w", service.ErrNotFound)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, partial := takeActionOutcome(actionResult(), tt.err)
			if status != http.StatusCreated {
				t.Errorf("status = %d, want %d — the action was already persisted", status, http.StatusCreated)
			}
			if !partial {
				t.Error("partial = false, want true so the caller logs the failure")
			}
		})
	}
}

package domain_test

import (
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
)

func TestUser_CanPost(t *testing.T) {
	tests := []struct {
		name     string
		user     domain.User
		expected bool
	}{
		{"active member with high trust", domain.User{IsActive: true, TrustScore: 50, Role: domain.RoleMember}, true},
		{"pending user", domain.User{IsActive: true, TrustScore: 50, Role: domain.RolePending}, false},
		{"low trust", domain.User{IsActive: true, TrustScore: 20, Role: domain.RoleMember}, false},
		{"banned", domain.User{IsActive: true, TrustScore: 80, Role: domain.RoleBanned}, false},
		{"inactive", domain.User{IsActive: false, TrustScore: 80, Role: domain.RoleMember}, false},
		{"exactly 30 trust", domain.User{IsActive: true, TrustScore: 30, Role: domain.RoleMember}, true},
		{"moderator can post", domain.User{IsActive: true, TrustScore: 50, Role: domain.RoleModerator}, true},
		{"council can post", domain.User{IsActive: true, TrustScore: 50, Role: domain.RoleCouncil}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.user.CanPost(); got != tt.expected {
				t.Errorf("CanPost() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestUser_CanVouch(t *testing.T) {
	tests := []struct {
		name     string
		user     domain.User
		expected bool
	}{
		{"high trust member", domain.User{IsActive: true, TrustScore: 70, Role: domain.RoleMember}, true},
		{"trust too low", domain.User{IsActive: true, TrustScore: 55, Role: domain.RoleMember}, false},
		{"exactly 60", domain.User{IsActive: true, TrustScore: 60, Role: domain.RoleMember}, true},
		{"banned with high trust", domain.User{IsActive: true, TrustScore: 80, Role: domain.RoleBanned}, false},
		{"pending with high trust", domain.User{IsActive: true, TrustScore: 80, Role: domain.RolePending}, false},
		{"inactive with high trust", domain.User{IsActive: false, TrustScore: 80, Role: domain.RoleMember}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.user.CanVouch(); got != tt.expected {
				t.Errorf("CanVouch() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestUser_CanModerate(t *testing.T) {
	tests := []struct {
		name     string
		user     domain.User
		expected bool
	}{
		{"moderator", domain.User{IsActive: true, Role: domain.RoleModerator}, true},
		{"council", domain.User{IsActive: true, Role: domain.RoleCouncil}, true},
		{"member", domain.User{IsActive: true, Role: domain.RoleMember}, false},
		{"pending", domain.User{IsActive: true, Role: domain.RolePending}, false},
		{"banned", domain.User{IsActive: true, Role: domain.RoleBanned}, false},
		{"inactive moderator", domain.User{IsActive: false, Role: domain.RoleModerator}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.user.CanModerate(); got != tt.expected {
				t.Errorf("CanModerate() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestUser_IsCouncil(t *testing.T) {
	tests := []struct {
		name     string
		user     domain.User
		expected bool
	}{
		{"council", domain.User{IsActive: true, Role: domain.RoleCouncil}, true},
		{"moderator", domain.User{IsActive: true, Role: domain.RoleModerator}, false},
		{"member", domain.User{IsActive: true, Role: domain.RoleMember}, false},
		{"inactive council", domain.User{IsActive: false, Role: domain.RoleCouncil}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.user.IsCouncil(); got != tt.expected {
				t.Errorf("IsCouncil() = %v, want %v", got, tt.expected)
			}
		})
	}
}

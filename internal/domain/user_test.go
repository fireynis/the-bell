package domain_test

import (
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// canPostNow is the reference instant for the mute boundary tests.
var canPostNow = time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

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
		{
			"muted member cannot post however high their trust",
			domain.User{IsActive: true, TrustScore: 95, Role: domain.RoleMember, MutedUntil: ptr(canPostNow.Add(time.Hour))},
			false,
		},
		{
			"a mute outranks moderator rank",
			domain.User{IsActive: true, TrustScore: 95, Role: domain.RoleModerator, MutedUntil: ptr(canPostNow.Add(time.Hour))},
			false,
		},
		{
			"expired mute no longer silences",
			domain.User{IsActive: true, TrustScore: 50, Role: domain.RoleMember, MutedUntil: ptr(canPostNow.Add(-time.Hour))},
			true,
		},
		{
			"nil muted_until is not a mute",
			domain.User{IsActive: true, TrustScore: 50, Role: domain.RoleMember, MutedUntil: nil},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.user.CanPost(canPostNow); got != tt.expected {
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

func ptr[T any](v T) *T { return &v }

// The mute boundary is exclusive at the expiry instant: at exactly muted_until
// the mute is over. A moderator setting a one-hour mute means the user posts
// again an hour later, not an hour and a tick.
func TestUser_IsMuted_Boundary(t *testing.T) {
	expiry := canPostNow

	tests := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"a nanosecond before expiry", expiry.Add(-time.Nanosecond), true},
		{"exactly at expiry", expiry, false},
		{"a nanosecond after expiry", expiry.Add(time.Nanosecond), false},
		{"long before", expiry.Add(-24 * time.Hour), true},
		{"long after", expiry.Add(24 * time.Hour), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := domain.User{MutedUntil: &expiry}
			if got := u.IsMuted(tt.now); got != tt.want {
				t.Errorf("IsMuted(%v) = %v, want %v", tt.now, got, tt.want)
			}
		})
	}
}

func TestUser_IsMuted_NilIsNeverMuted(t *testing.T) {
	u := domain.User{}
	for _, now := range []time.Time{{}, canPostNow, canPostNow.Add(100 * 365 * 24 * time.Hour)} {
		if u.IsMuted(now) {
			t.Errorf("IsMuted(%v) = true with a nil muted_until", now)
		}
	}
}

// A mute is not the only thing that stops a post, and it must not be able to
// re-enable one: a banned or deactivated account stays unable to post whether
// or not its mute has expired.
func TestUser_CanPost_MuteDoesNotOverrideOtherGates(t *testing.T) {
	past := canPostNow.Add(-time.Hour)

	tests := []struct {
		name string
		user domain.User
	}{
		{"banned with an expired mute", domain.User{IsActive: true, TrustScore: 80, Role: domain.RoleBanned, MutedUntil: &past}},
		{"deactivated with an expired mute", domain.User{IsActive: false, TrustScore: 80, Role: domain.RoleMember, MutedUntil: &past}},
		{"pending with an expired mute", domain.User{IsActive: true, TrustScore: 80, Role: domain.RolePending, MutedUntil: &past}},
		{"low trust with an expired mute", domain.User{IsActive: true, TrustScore: 10, Role: domain.RoleMember, MutedUntil: &past}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.user.CanPost(canPostNow) {
				t.Error("CanPost() = true; an expired mute revived an account another gate had stopped")
			}
		})
	}
}

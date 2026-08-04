package handler

import (
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

func TestToProfileResponse(t *testing.T) {
	joined := time.Date(2026, 3, 1, 12, 30, 45, 0, time.UTC)

	tests := []struct {
		name string
		user *domain.User
		want userProfileResponse
	}{
		{
			"full profile",
			&domain.User{
				ID:          "user-1",
				DisplayName: "Ada",
				Bio:         "builds things",
				AvatarURL:   "/img/ada.jpg",
				TrustScore:  72.5,
				Role:        domain.RoleModerator,
				IsActive:    true,
				JoinedAt:    joined,
			},
			userProfileResponse{
				ID:          "user-1",
				DisplayName: "Ada",
				Bio:         "builds things",
				AvatarURL:   "/img/ada.jpg",
				TrustScore:  72.5,
				Role:        "moderator",
				IsActive:    true,
				JoinedAt:    "2026-03-01T12:30:45Z",
			},
		},
		{
			"empty optional fields stay empty rather than becoming null",
			&domain.User{ID: "user-2", Role: domain.RolePending, JoinedAt: joined},
			userProfileResponse{ID: "user-2", Role: "pending", JoinedAt: "2026-03-01T12:30:45Z"},
		},
		{
			"deactivated banned user",
			&domain.User{ID: "user-3", Role: domain.RoleBanned, IsActive: false, JoinedAt: joined},
			userProfileResponse{ID: "user-3", Role: "banned", JoinedAt: "2026-03-01T12:30:45Z"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toProfileResponse(tt.user); got != tt.want {
				t.Errorf("toProfileResponse() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestToVouchEntry(t *testing.T) {
	created := time.Date(2026, 3, 1, 12, 30, 45, 0, time.UTC)
	revoked := created.Add(time.Hour)

	tests := []struct {
		name  string
		vouch *domain.Vouch
		want  vouchEntry
	}{
		{
			"active vouch",
			&domain.Vouch{
				ID:        "vouch-1",
				VoucherID: "user-1",
				VoucheeID: "user-2",
				Status:    domain.VouchActive,
				CreatedAt: created,
			},
			vouchEntry{
				ID:        "vouch-1",
				VoucherID: "user-1",
				VoucheeID: "user-2",
				Status:    "active",
				CreatedAt: "2026-03-01T12:30:45Z",
			},
		},
		{
			// A revoked vouch still reports when it was given, not when it was
			// revoked; the profile lists the vouch history, not its end state.
			"revoked vouch keeps its creation time",
			&domain.Vouch{
				ID:        "vouch-2",
				VoucherID: "user-1",
				VoucheeID: "user-3",
				Status:    domain.VouchRevoked,
				CreatedAt: created,
				RevokedAt: &revoked,
			},
			vouchEntry{
				ID:        "vouch-2",
				VoucherID: "user-1",
				VoucheeID: "user-3",
				Status:    "revoked",
				CreatedAt: "2026-03-01T12:30:45Z",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toVouchEntry(tt.vouch); got != tt.want {
				t.Errorf("toVouchEntry() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// Profiles and vouches are rendered by the same frontend date parsing, so both
// must emit RFC 3339 with the zone the timestamp carries. A timestamp stored in
// a non-UTC zone must keep its offset rather than silently shifting.
func TestTimestampFormat_IsSharedByProfilesAndVouches(t *testing.T) {
	newfoundland := time.FixedZone("NST", -3*3600-1800)
	ts := time.Date(2026, 3, 1, 12, 30, 45, 0, newfoundland)
	const want = "2026-03-01T12:30:45-03:30"

	if got := toProfileResponse(&domain.User{JoinedAt: ts}).JoinedAt; got != want {
		t.Errorf("joined_at = %q, want %q", got, want)
	}
	if got := toVouchEntry(&domain.Vouch{CreatedAt: ts}).CreatedAt; got != want {
		t.Errorf("created_at = %q, want %q", got, want)
	}
}

// The zero time is what an unset column decodes to; it must still render as a
// parseable timestamp rather than an empty string the frontend would choke on.
func TestTimestampFormat_ZeroTime(t *testing.T) {
	const want = "0001-01-01T00:00:00Z"

	if got := toProfileResponse(&domain.User{}).JoinedAt; got != want {
		t.Errorf("joined_at = %q, want %q", got, want)
	}
	if got := toVouchEntry(&domain.Vouch{}).CreatedAt; got != want {
		t.Errorf("created_at = %q, want %q", got, want)
	}
}

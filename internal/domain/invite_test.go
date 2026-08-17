package domain

import (
	"testing"
	"time"
)

func TestInvite_Status(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	created := now.Add(-24 * time.Hour)
	expiresLater := now.Add(13 * 24 * time.Hour)
	expiredAlready := now.Add(-time.Hour)

	at := func(d time.Duration) *time.Time {
		t := now.Add(d)
		return &t
	}

	tests := []struct {
		name   string
		invite Invite
		want   InviteStatus
	}{
		{
			name:   "unanswered and still in date",
			invite: Invite{CreatedAt: created, ExpiresAt: expiresLater},
			want:   InviteOpen,
		},
		{
			name:   "past its expiry",
			invite: Invite{CreatedAt: created, ExpiresAt: expiredAlready},
			want:   InviteExpired,
		},
		{
			name:   "exactly at its expiry is over",
			invite: Invite{CreatedAt: created, ExpiresAt: now},
			want:   InviteExpired,
		},
		{
			name:   "withdrawn while it was still open",
			invite: Invite{CreatedAt: created, ExpiresAt: expiresLater, RevokedAt: at(-time.Hour)},
			want:   InviteRevoked,
		},
		{
			name: "reaped after expiry still reads as expired",
			// This is what freeing an address for a fresh invitation writes: a
			// revoked_at stamped at a moment already past expires_at. The
			// inviter did not withdraw it, so it must not say they did.
			invite: Invite{CreatedAt: created, ExpiresAt: expiredAlready, RevokedAt: at(0)},
			want:   InviteExpired,
		},
		{
			name:   "accepted",
			invite: Invite{CreatedAt: created, ExpiresAt: expiresLater, ConsumedAt: at(-time.Hour)},
			want:   InviteAccepted,
		},
		{
			name: "accepted, and only later out of date",
			// Acceptance is terminal. Whether the expiry has since passed says
			// nothing about an invitation somebody already used.
			invite: Invite{CreatedAt: created, ExpiresAt: expiredAlready, ConsumedAt: at(-2 * time.Hour)},
			want:   InviteAccepted,
		},
		{
			name: "accepted beats a revocation recorded afterwards",
			invite: Invite{CreatedAt: created, ExpiresAt: expiresLater,
				ConsumedAt: at(-2 * time.Hour), RevokedAt: at(-time.Hour)},
			want: InviteAccepted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.invite.Status(now); got != tt.want {
				t.Errorf("Status() = %q, want %q", got, tt.want)
			}
		})
	}
}

// IsLive is the Go statement of the liveness clause the queries apply, so it
// has to admit exactly the invitations they do: only an open one.
func TestInvite_IsLive_OnlyForOpenInvitations(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	earlier := now.Add(-time.Hour)

	open := Invite{CreatedAt: now.Add(-24 * time.Hour), ExpiresAt: now.Add(24 * time.Hour)}
	if !open.IsLive(now) {
		t.Error("an open invitation is not live")
	}

	for name, invite := range map[string]Invite{
		"expired":  {ExpiresAt: earlier},
		"revoked":  {ExpiresAt: now.Add(24 * time.Hour), RevokedAt: &earlier},
		"accepted": {ExpiresAt: now.Add(24 * time.Hour), ConsumedAt: &earlier},
	} {
		if invite.IsLive(now) {
			t.Errorf("%s invitation reports live", name)
		}
	}
}

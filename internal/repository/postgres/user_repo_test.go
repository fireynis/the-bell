package postgres

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// userRow is a stored user with only the columns these tests vary.
func userRow(active bool, suspendedUntil *time.Time) User {
	row := User{
		ID:       "user-1",
		Role:     "member",
		IsActive: active,
	}
	if suspendedUntil != nil {
		row.SuspendedUntil = pgtype.Timestamptz{Time: *suspendedUntil, Valid: true}
	}
	return row
}

// Hydration is where a suspension reaches every gate in the codebase. CanVouch,
// CanModerate and the RequireActive middleware all decide from IsActive and
// none of them holds a clock, so folding the suspension in here is what makes
// one expire by itself everywhere at once.
func TestUserFromRow_SuspensionFoldsIntoIsActive(t *testing.T) {
	future := time.Now().Add(24 * time.Hour)
	past := time.Now().Add(-time.Nanosecond)

	tests := []struct {
		name         string
		row          User
		wantIsActive bool
	}{
		{"no suspension", userRow(true, nil), true},
		{"suspension still in force", userRow(true, &future), false},
		// The bug this replaced: a suspension whose time had passed went on
		// applying, because is_active had been cleared and nothing set it back.
		{"suspension lapsed", userRow(true, &past), true},
		{"deactivated account", userRow(false, nil), false},
		// is_active means something of its own, and a lapsed suspension must
		// not reactivate an account that was taken out of service separately.
		{"deactivated with a lapsed suspension", userRow(false, &past), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := userFromRow(tt.row).IsActive; got != tt.wantIsActive {
				t.Errorf("IsActive = %v, want %v", got, tt.wantIsActive)
			}
		})
	}
}

// The timestamp itself survives hydration, so a moderator's view can say when
// the suspension ends rather than only that the account cannot act.
func TestUserFromRow_CarriesSuspendedUntil(t *testing.T) {
	until := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	u := userFromRow(userRow(true, &until))
	if u.SuspendedUntil == nil {
		t.Fatal("SuspendedUntil = nil, want the stored expiry")
	}
	if !u.SuspendedUntil.Equal(until) {
		t.Errorf("SuspendedUntil = %v, want %v", *u.SuspendedUntil, until)
	}

	if unsuspended := userFromRow(userRow(true, nil)); unsuspended.SuspendedUntil != nil {
		t.Errorf("SuspendedUntil = %v for a row with no suspension, want nil", *unsuspended.SuspendedUntil)
	}
}

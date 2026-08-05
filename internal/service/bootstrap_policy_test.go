package service

import (
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// The council seeds the trust graph, so setup has to grant it standing that no
// other route into the town can produce: maximum trust with nobody vouching,
// the council role, and an account that is usable straight away.
func TestNewCouncilUser(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	user := newCouncilUser("user-1", "kratos-1", "alice@town.example", now)

	if user.ID != "user-1" {
		t.Errorf("ID = %q, want %q", user.ID, "user-1")
	}
	if user.KratosIdentityID != "kratos-1" {
		t.Errorf("KratosIdentityID = %q, want %q", user.KratosIdentityID, "kratos-1")
	}
	if user.TrustScore != 100.0 {
		t.Errorf("TrustScore = %v, want 100", user.TrustScore)
	}
	if user.Role != domain.RoleCouncil {
		t.Errorf("Role = %q, want %q", user.Role, domain.RoleCouncil)
	}
	if !user.IsActive {
		t.Error("IsActive = false, want a council member usable immediately")
	}
	if user.TrustBelowSince != nil {
		t.Errorf("TrustBelowSince = %v, want nil for a brand-new account", user.TrustBelowSince)
	}
}

// Setup knows only the email, so it doubles as the display name until the
// council member edits their profile.
func TestNewCouncilUser_EmailIsTheInitialDisplayName(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	for _, email := range []string{"alice@town.example", "mayor+bell@town.example"} {
		if got := newCouncilUser("user-1", "kratos-1", email, now).DisplayName; got != email {
			t.Errorf("DisplayName = %q, want %q", got, email)
		}
	}
}

// All three timestamps come from the same clock reading. A council member's
// tenure is what the promotion policy measures, so JoinedAt must not drift from
// the creation time.
func TestNewCouncilUser_TimestampsShareOneClockReading(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	user := newCouncilUser("user-1", "kratos-1", "alice@town.example", now)

	for _, ts := range []struct {
		name string
		got  time.Time
	}{
		{"JoinedAt", user.JoinedAt},
		{"CreatedAt", user.CreatedAt},
		{"UpdatedAt", user.UpdatedAt},
	} {
		if !ts.got.Equal(now) {
			t.Errorf("%s = %v, want %v", ts.name, ts.got, now)
		}
	}
}

// The trust and role a council member starts with must clear every permission
// gate; a council account that could not post or vouch would leave a fresh town
// with nobody able to act.
func TestNewCouncilUser_ClearsEveryPermissionGate(t *testing.T) {
	user := newCouncilUser("user-1", "kratos-1", "alice@town.example", time.Now())

	if !user.CanPost(time.Now()) {
		t.Error("CanPost() = false, want a council member able to post")
	}
	if !user.CanVouch() {
		t.Error("CanVouch() = false, want a council member able to vouch others in")
	}
	if !user.CanModerate() {
		t.Error("CanModerate() = false, want a council member able to moderate")
	}
	if !user.IsCouncil() {
		t.Error("IsCouncil() = false, want a council member")
	}
}

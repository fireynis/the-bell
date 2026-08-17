package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
)

// Becoming a member is a role change, and role_history is where this codebase
// records role changes. Until these two paths wrote entries, the trail held
// automatic promotions and council votes and nothing else — so the two ways
// somebody actually joins a town, being vouched for and being approved during
// bootstrap, were exactly the changes it could not explain.

func TestVouchService_Vouch_RecordsThePromotionInRoleHistory(t *testing.T) {
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 80.0)
	users.users["vouchee-1"] = pendingUser("vouchee-1")
	history := &fakeRoleHistory{}

	svc := NewVouchService(repo, graph, users, fixedClock)
	svc.SetRoleHistory(history)

	vouch, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if err != nil {
		t.Fatalf("Vouch() unexpected error: %v", err)
	}

	if len(history.entries) != 1 {
		t.Fatalf("recorded %d role history entries, want 1", len(history.entries))
	}
	entry := history.entries[0]
	if entry.UserID != "vouchee-1" {
		t.Errorf("user_id = %q, want the vouchee", entry.UserID)
	}
	if entry.OldRole != domain.RolePending || entry.NewRole != domain.RoleMember {
		t.Errorf("role change = %q -> %q, want pending -> member", entry.OldRole, entry.NewRole)
	}
	if entry.CreatedAt != fixedNow {
		t.Errorf("created_at = %v, want the service clock's %v", entry.CreatedAt, fixedNow)
	}
	// The reason has to say which vouch, because that is the only pointer back
	// to who did it — the row itself names nobody but the person promoted.
	if !strings.Contains(entry.Reason, vouch.ID) {
		t.Errorf("reason = %q, want it to name vouch %s", entry.Reason, vouch.ID)
	}
	if !strings.Contains(entry.Reason, "vouch") {
		t.Errorf("reason = %q, want it to name the mechanism", entry.Reason)
	}
}

func TestVouchService_Vouch_RecordsNothingWhenNobodyIsPromoted(t *testing.T) {
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 80.0)
	// Already a member: a second vouch endorses them, it does not promote them.
	users.users["vouchee-1"] = activeMember("vouchee-1", 55.0)
	history := &fakeRoleHistory{}

	svc := NewVouchService(repo, graph, users, fixedClock)
	svc.SetRoleHistory(history)

	if _, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1"); err != nil {
		t.Fatalf("Vouch() unexpected error: %v", err)
	}

	if len(history.entries) != 0 {
		t.Errorf("recorded %d entries for a vouch that changed no role, want 0", len(history.entries))
	}
}

// The vouch and the promotion have already committed by the time the audit
// entry is written, so the failure has to be reported rather than swallowed —
// and reported as a server error, not as something the voucher did wrong.
func TestVouchService_Vouch_ReportsAFailedRoleHistoryWrite(t *testing.T) {
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 80.0)
	users.users["vouchee-1"] = pendingUser("vouchee-1")
	history := &fakeRoleHistory{err: errors.New("audit table unavailable")}

	svc := NewVouchService(repo, graph, users, fixedClock)
	svc.SetRoleHistory(history)

	vouch, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1")
	if err == nil {
		t.Fatal("Vouch() error = nil, want the audit failure reported")
	}
	if vouch == nil {
		t.Error("Vouch() returned no vouch; the caller cannot tell what did persist")
	}
	if users.updatedRoles["vouchee-1"] != domain.RoleMember {
		t.Error("the promotion was rolled back; it should stand")
	}
	for _, sentinel := range []error{ErrNotFound, ErrValidation, ErrForbidden} {
		if errors.Is(err, sentinel) {
			t.Errorf("error unwraps to %v, which would render as the caller's mistake", sentinel)
		}
	}
}

// A service wired without a writer still promotes. The warning is the signal;
// refusing the promotion would make a missing audit entry cost somebody their
// membership.
func TestVouchService_Vouch_PromotesWithoutARoleHistoryWriter(t *testing.T) {
	repo := newMockVouchRepo()
	graph := newMockGraph()
	users := newMockUserGetter()
	users.users["voucher-1"] = activeMember("voucher-1", 80.0)
	users.users["vouchee-1"] = pendingUser("vouchee-1")

	svc := NewVouchService(repo, graph, users, fixedClock)
	svc.logger = discardLogger()

	if _, err := svc.Vouch(context.Background(), "voucher-1", "vouchee-1"); err != nil {
		t.Fatalf("Vouch() unexpected error: %v", err)
	}
	if users.updatedRoles["vouchee-1"] != domain.RoleMember {
		t.Error("the vouchee was not promoted")
	}
}

func TestApprovalService_Approve_RecordsThePromotionInRoleHistory(t *testing.T) {
	users := newFakeUserStore()
	users.add(&domain.User{ID: "pending-1", Role: domain.RolePending, IsActive: true})
	config := newMockConfigRepo()
	config.config["bootstrap_mode"] = "true"
	history := &fakeRoleHistory{}

	svc := NewApprovalService(users, config)
	svc.SetRoleHistory(history)
	svc.SetClock(fixedClock)

	if _, err := svc.Approve(context.Background(), "pending-1"); err != nil {
		t.Fatalf("Approve() unexpected error: %v", err)
	}

	if len(history.entries) != 1 {
		t.Fatalf("recorded %d role history entries, want 1", len(history.entries))
	}
	entry := history.entries[0]
	if entry.UserID != "pending-1" {
		t.Errorf("user_id = %q", entry.UserID)
	}
	if entry.OldRole != domain.RolePending || entry.NewRole != domain.RoleMember {
		t.Errorf("role change = %q -> %q, want pending -> member", entry.OldRole, entry.NewRole)
	}
	if entry.CreatedAt != fixedNow {
		t.Errorf("created_at = %v, want the service clock's %v", entry.CreatedAt, fixedNow)
	}
	if !strings.Contains(entry.Reason, "council approval") {
		t.Errorf("reason = %q, want it to name the mechanism", entry.Reason)
	}
}

func TestApprovalService_Approve_ReportsAFailedRoleHistoryWrite(t *testing.T) {
	users := newFakeUserStore()
	users.add(&domain.User{ID: "pending-1", Role: domain.RolePending, IsActive: true})
	config := newMockConfigRepo()
	config.config["bootstrap_mode"] = "true"

	svc := NewApprovalService(users, config)
	svc.SetRoleHistory(&fakeRoleHistory{err: errors.New("audit table unavailable")})

	_, err := svc.Approve(context.Background(), "pending-1")
	if err == nil {
		t.Fatal("Approve() error = nil, want the audit failure reported")
	}
	if users.users["pending-1"].Role != domain.RoleMember {
		t.Error("the promotion was rolled back; it should stand")
	}
	for _, sentinel := range []error{ErrNotFound, ErrValidation, ErrForbidden} {
		if errors.Is(err, sentinel) {
			t.Errorf("error unwraps to %v, which would render as the caller's mistake", sentinel)
		}
	}
}

func TestApprovalService_Approve_PromotesWithoutARoleHistoryWriter(t *testing.T) {
	users := newFakeUserStore()
	users.add(&domain.User{ID: "pending-1", Role: domain.RolePending, IsActive: true})
	config := newMockConfigRepo()
	config.config["bootstrap_mode"] = "true"

	svc := NewApprovalService(users, config)
	svc.logger = discardLogger()

	if _, err := svc.Approve(context.Background(), "pending-1"); err != nil {
		t.Fatalf("Approve() unexpected error: %v", err)
	}
	if users.users["pending-1"].Role != domain.RoleMember {
		t.Error("the user was not promoted")
	}
}

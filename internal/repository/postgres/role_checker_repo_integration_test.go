//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/fireynis/the-bell/internal/testsupport"
)

// These six adapters drive `bell check-roles`, which promotes and demotes people
// without anyone in the loop. A wrong candidate list or a mis-scoped vouch count
// silently changes someone's standing in the town, so every one is checked
// against a real database.

func roleUsers(users []service.RoleCheckerUser) map[string]service.RoleCheckerUser {
	byID := make(map[string]service.RoleCheckerUser, len(users))
	for _, u := range users {
		byID[u.ID] = u
	}
	return byID
}

// The candidate list is who gets considered at all. Pending and banned users are
// excluded despite the method's name mentioning only banned, and so is anyone
// deactivated — a demotion applied to a departed account would be noise, and a
// promotion would be worse.
func TestRoleCheckerRepo_ListActiveNonBannedUsers_Selection(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewRoleCheckerRepo(postgres.New(pool))
	userRepo := postgres.NewUserRepo(postgres.New(pool))

	member := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rc-member"), domain.RoleMember, 50)
	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rc-mod"), domain.RoleModerator, 88)
	council := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rc-council"), domain.RoleCouncil, 95)
	pending := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rc-pending"), domain.RolePending, 50)
	banned := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rc-banned"), domain.RoleBanned, 5)

	inactive := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rc-inactive"), domain.RoleMember, 50)
	if err := userRepo.DeactivateUser(ctx, inactive.ID); err != nil {
		t.Fatalf("DeactivateUser: %v", err)
	}

	got, err := repo.ListActiveNonBannedUsers(ctx)
	if err != nil {
		t.Fatalf("ListActiveNonBannedUsers: %v", err)
	}
	byID := roleUsers(got)

	for _, want := range []*domain.User{member, moderator, council} {
		if _, ok := byID[want.ID]; !ok {
			t.Errorf("%s (%s) missing from the candidate list", want.ID, want.Role)
		}
	}
	for name, excluded := range map[string]*domain.User{
		"pending":     pending,
		"banned":      banned,
		"deactivated": inactive,
	} {
		if _, ok := byID[excluded.ID]; ok {
			t.Errorf("%s user %s is in the candidate list", name, excluded.ID)
		}
	}
	if len(got) != 3 {
		t.Errorf("candidate list has %d users, want 3", len(got))
	}
}

// The fields the promotion rules read must survive the round trip: a zero
// JoinedAt would make everyone look brand new and block every promotion.
func TestRoleCheckerRepo_ListActiveNonBannedUsers_PopulatesDecisionFields(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewRoleCheckerRepo(postgres.New(pool))

	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rc-fields"), domain.RoleMember, 42.5)

	got, err := repo.ListActiveNonBannedUsers(ctx)
	if err != nil {
		t.Fatalf("ListActiveNonBannedUsers: %v", err)
	}
	found, ok := roleUsers(got)[user.ID]
	if !ok {
		t.Fatalf("user %s missing from the candidate list", user.ID)
	}

	if found.TrustScore != 42.5 {
		t.Errorf("trust score = %v, want 42.5", found.TrustScore)
	}
	if found.Role != domain.RoleMember {
		t.Errorf("role = %q, want %q", found.Role, domain.RoleMember)
	}
	if found.JoinedAt.IsZero() {
		t.Error("joined_at is zero: every promotion check would see a brand-new account")
	}
	// Nobody has been below the threshold yet, so the pointer stays nil rather
	// than becoming a zero time that would read as "below since year 1".
	if found.TrustBelowSince != nil {
		t.Errorf("trust_below_since = %v, want nil", *found.TrustBelowSince)
	}
}

// The demotion clock: set it, read it back through the candidate list, then
// clear it when the user recovers.
func TestRoleCheckerRepo_TrustBelowSince_SetAndClear(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewRoleCheckerRepo(postgres.New(pool))

	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rc-clock"), domain.RoleModerator, 65)
	since := time.Now().Add(-10 * 24 * time.Hour).Truncate(time.Millisecond)

	if err := repo.UpdateUserTrustBelowSince(ctx, user.ID, since); err != nil {
		t.Fatalf("UpdateUserTrustBelowSince: %v", err)
	}

	found := mustFindRoleUser(t, repo, user.ID)
	if found.TrustBelowSince == nil {
		t.Fatal("trust_below_since is nil after being set")
	}
	if diff := found.TrustBelowSince.Sub(since).Abs(); diff > time.Millisecond {
		t.Errorf("trust_below_since = %v, want ~%v (off by %v)", *found.TrustBelowSince, since, diff)
	}

	// Setting it again overwrites rather than accumulating.
	newer := since.Add(24 * time.Hour)
	if err := repo.UpdateUserTrustBelowSince(ctx, user.ID, newer); err != nil {
		t.Fatalf("UpdateUserTrustBelowSince: %v", err)
	}
	found = mustFindRoleUser(t, repo, user.ID)
	if diff := found.TrustBelowSince.Sub(newer).Abs(); diff > time.Millisecond {
		t.Errorf("trust_below_since = %v, want ~%v", *found.TrustBelowSince, newer)
	}

	if err := repo.ClearUserTrustBelowSince(ctx, user.ID); err != nil {
		t.Fatalf("ClearUserTrustBelowSince: %v", err)
	}
	found = mustFindRoleUser(t, repo, user.ID)
	if found.TrustBelowSince != nil {
		t.Errorf("trust_below_since = %v after clearing, want nil", *found.TrustBelowSince)
	}
}

func mustFindRoleUser(t *testing.T, repo *postgres.RoleCheckerRepo, id string) service.RoleCheckerUser {
	t.Helper()

	users, err := repo.ListActiveNonBannedUsers(context.Background())
	if err != nil {
		t.Fatalf("ListActiveNonBannedUsers: %v", err)
	}
	found, ok := roleUsers(users)[id]
	if !ok {
		t.Fatalf("user %s missing from the candidate list", id)
	}
	return found
}

// Updating a user who does not exist changes nothing and is not an error — the
// check runs over a list read earlier, so a user deleted in between must not
// abort the whole sweep.
func TestRoleCheckerRepo_TrustBelowSince_UnknownUserIsNoop(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewRoleCheckerRepo(postgres.New(pool))

	if err := repo.UpdateUserTrustBelowSince(ctx, "no-such-user", time.Now()); err != nil {
		t.Errorf("UpdateUserTrustBelowSince on an unknown user: %v", err)
	}
	if err := repo.ClearUserTrustBelowSince(ctx, "no-such-user"); err != nil {
		t.Errorf("ClearUserTrustBelowSince on an unknown user: %v", err)
	}
	if err := repo.UpdateUserRole(ctx, "no-such-user", domain.RoleModerator); err != nil {
		t.Errorf("UpdateUserRole on an unknown user: %v", err)
	}
}

// Promotion to moderator requires vouches from people who already hold power.
// This count must ignore vouches from ordinary members, revoked vouches, and
// vouches from a moderator whose own account has been deactivated.
func TestRoleCheckerRepo_CountActiveModeratorVouchesForUser(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewRoleCheckerRepo(postgres.New(pool))
	vouches := postgres.NewVouchRepo(postgres.New(pool))
	users := postgres.NewUserRepo(postgres.New(pool))

	candidate := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rc-candidate"), domain.RoleMember, 88)
	mod := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rc-voucher-mod"), domain.RoleModerator, 90)
	council := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rc-voucher-council"), domain.RoleCouncil, 95)
	member := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rc-voucher-member"), domain.RoleMember, 70)
	revoker := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rc-voucher-revoked"), domain.RoleModerator, 90)
	departed := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rc-voucher-gone"), domain.RoleModerator, 90)

	now := time.Now()
	addVouch(t, vouches, "rcv-mod", mod.ID, candidate.ID, domain.VouchActive, now)
	addVouch(t, vouches, "rcv-council", council.ID, candidate.ID, domain.VouchActive, now)
	addVouch(t, vouches, "rcv-member", member.ID, candidate.ID, domain.VouchActive, now)
	addVouch(t, vouches, "rcv-revoked", revoker.ID, candidate.ID, domain.VouchRevoked, now)
	addVouch(t, vouches, "rcv-departed", departed.ID, candidate.ID, domain.VouchActive, now)
	if err := users.DeactivateUser(ctx, departed.ID); err != nil {
		t.Fatalf("DeactivateUser: %v", err)
	}

	// A vouch the candidate gave to a moderator is the wrong direction and must
	// not count toward their own promotion.
	addVouch(t, vouches, "rcv-outgoing", candidate.ID, mod.ID, domain.VouchActive, now)

	got, err := repo.CountActiveModeratorVouchesForUser(ctx, candidate.ID)
	if err != nil {
		t.Fatalf("CountActiveModeratorVouchesForUser: %v", err)
	}
	if got != 2 {
		t.Errorf("count = %d, want 2 (the active moderator and council vouches)", got)
	}

	if got, err := repo.CountActiveModeratorVouchesForUser(ctx, "no-such-user"); err != nil || got != 0 {
		t.Errorf("count for an unknown user = %d, %v; want 0, nil", got, err)
	}
}

func TestRoleCheckerRepo_UpdateUserRole(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewRoleCheckerRepo(postgres.New(pool))
	users := postgres.NewUserRepo(postgres.New(pool))

	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rc-promote"), domain.RoleMember, 90)

	if err := repo.UpdateUserRole(ctx, user.ID, domain.RoleModerator); err != nil {
		t.Fatalf("UpdateUserRole: %v", err)
	}

	got, err := users.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if got.Role != domain.RoleModerator {
		t.Errorf("role = %q, want %q", got.Role, domain.RoleModerator)
	}
}

// Every automatic role change is meant to leave an audit trail, so the history
// row has to persist with both the old and the new role.
func TestRoleCheckerRepo_CreateRoleHistoryEntry(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewRoleCheckerRepo(postgres.New(pool))

	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rc-history"), domain.RoleMember, 90)
	createdAt := time.Now().Truncate(time.Millisecond)

	entry := &domain.RoleHistory{
		ID:        "role-history-1",
		UserID:    user.ID,
		OldRole:   domain.RoleMember,
		NewRole:   domain.RoleModerator,
		Reason:    "trust 90.0 for 90 days with 2 moderator vouches",
		CreatedAt: createdAt,
	}
	if err := repo.CreateRoleHistoryEntry(ctx, entry); err != nil {
		t.Fatalf("CreateRoleHistoryEntry: %v", err)
	}

	var (
		oldRole, newRole, reason string
		stored                   time.Time
	)
	err := pool.QueryRow(ctx,
		"SELECT old_role, new_role, reason, created_at FROM role_history WHERE id = $1", entry.ID,
	).Scan(&oldRole, &newRole, &reason, &stored)
	if err != nil {
		t.Fatalf("reading role_history: %v", err)
	}

	if oldRole != string(domain.RoleMember) || newRole != string(domain.RoleModerator) {
		t.Errorf("stored %q -> %q, want %q -> %q", oldRole, newRole, domain.RoleMember, domain.RoleModerator)
	}
	if reason != entry.Reason {
		t.Errorf("reason = %q, want %q", reason, entry.Reason)
	}
	if diff := stored.Sub(createdAt).Abs(); diff > time.Millisecond {
		t.Errorf("created_at = %v, want ~%v", stored, createdAt)
	}
}

// The history table references users(id); an entry for a user who does not
// exist must be refused rather than silently recorded against nobody.
func TestRoleCheckerRepo_CreateRoleHistoryEntry_UnknownUserIsRejected(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewRoleCheckerRepo(postgres.New(pool))

	err := repo.CreateRoleHistoryEntry(ctx, &domain.RoleHistory{
		ID:        "role-history-orphan",
		UserID:    "no-such-user",
		OldRole:   domain.RoleMember,
		NewRole:   domain.RoleModerator,
		CreatedAt: time.Now(),
	})
	if err == nil {
		t.Error("an orphaned role history entry was accepted")
	}
}

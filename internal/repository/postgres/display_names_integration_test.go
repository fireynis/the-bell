//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/testsupport"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The vouch list, the moderation queue and the audit trail all showed raw
// UUIDs, and all three now join users for the name. A join is exactly the kind
// of change a unit test cannot check: the alias could name the wrong side of the
// edge, or the join could silently drop rows. These run against a real database
// for that reason.

// nameUser sets a display name on an existing fixture user. testsupport.TestUser
// deliberately creates them blank — that is the pre-backfill member every one of
// these reads has to tolerate — so a test that wants a name asks for one.
func nameUser(t *testing.T, pool *pgxpool.Pool, userID, name string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`UPDATE users SET display_name = $2 WHERE id = $1`, userID, name)
	if err != nil {
		t.Fatalf("naming user %s: %v", userID, err)
	}
}

// Both sides of the edge, in both directions. An alias pointing at the wrong
// side would still produce two names and still look right in a unit test, so
// what is pinned here is that the voucher's name is the voucher's.
func TestVouchRepo_ListActiveVouches_NamesBothParties(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewVouchRepo(postgres.New(pool))

	voucher := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("vouch-name-er"), domain.RoleMember, 80)
	vouchee := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("vouch-name-ee"), domain.RoleMember, 50)
	nameUser(t, pool, voucher.ID, "Alice")
	nameUser(t, pool, vouchee.ID, "Bob")

	err := repo.CreateVouch(ctx, &domain.Vouch{
		ID:        uuid.NewString(),
		VoucherID: voucher.ID,
		VoucheeID: vouchee.ID,
		Status:    domain.VouchActive,
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("CreateVouch: %v", err)
	}

	received, err := repo.ListActiveVouchesByVouchee(ctx, vouchee.ID)
	if err != nil {
		t.Fatalf("ListActiveVouchesByVouchee: %v", err)
	}
	if len(received) != 1 {
		t.Fatalf("%d received vouches, want 1", len(received))
	}
	if received[0].VoucherDisplayName != "Alice" || received[0].VoucheeDisplayName != "Bob" {
		t.Errorf("received names = %q/%q, want Alice/Bob",
			received[0].VoucherDisplayName, received[0].VoucheeDisplayName)
	}

	given, err := repo.ListActiveVouchesByVoucher(ctx, voucher.ID)
	if err != nil {
		t.Fatalf("ListActiveVouchesByVoucher: %v", err)
	}
	if len(given) != 1 {
		t.Fatalf("%d given vouches, want 1", len(given))
	}
	if given[0].VoucherDisplayName != "Alice" || given[0].VoucheeDisplayName != "Bob" {
		t.Errorf("given names = %q/%q, want Alice/Bob",
			given[0].VoucherDisplayName, given[0].VoucheeDisplayName)
	}
}

// A member who has set no display name yet is the common case in a town that
// predates the name trait. The join must return their vouch with an empty name
// rather than dropping the row, which is why it is an inner join on a NOT NULL
// column and not something cleverer.
func TestVouchRepo_ListActiveVouches_UnnamedMemberStillListed(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewVouchRepo(postgres.New(pool))

	voucher := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("vouch-blank-er"), domain.RoleMember, 80)
	vouchee := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("vouch-blank-ee"), domain.RoleMember, 50)

	err := repo.CreateVouch(ctx, &domain.Vouch{
		ID:        uuid.NewString(),
		VoucherID: voucher.ID,
		VoucheeID: vouchee.ID,
		Status:    domain.VouchActive,
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("CreateVouch: %v", err)
	}

	got, err := repo.ListActiveVouchesByVouchee(ctx, vouchee.ID)
	if err != nil {
		t.Fatalf("ListActiveVouchesByVouchee: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("%d vouches for a pair with no display names, want 1", len(got))
	}
	if got[0].VoucherDisplayName != "" || got[0].VoucheeDisplayName != "" {
		t.Errorf("names = %q/%q, want both empty",
			got[0].VoucherDisplayName, got[0].VoucheeDisplayName)
	}
}

// The moderation queue names whoever filed each report.
func TestReportRepo_ListPendingReports_NamesTheReporter(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewReportRepo(postgres.New(pool))

	author := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("qname-author"), domain.RoleMember, 50)
	reporter := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("qname-reporter"), domain.RoleMember, 50)
	unnamed := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("qname-unnamed"), domain.RoleMember, 50)
	nameUser(t, pool, reporter.ID, "Alice")

	newPost(t, pool, "qname-post-1", author.ID, domain.PostVisible, time.Now())
	newPost(t, pool, "qname-post-2", author.ID, domain.PostVisible, time.Now())
	newReport(t, pool, reporter.ID, "qname-post-1", "pending", time.Now().Add(-time.Hour))
	newReport(t, pool, unnamed.ID, "qname-post-2", "pending", time.Now())

	got, err := repo.ListPendingReports(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListPendingReports: %v", err)
	}
	// A reporter with no name must not cost the queue their report: the
	// moderator still has to see it.
	if len(got) != 2 {
		t.Fatalf("%d pending reports, want 2", len(got))
	}
	if got[0].ReporterDisplayName != "Alice" {
		t.Errorf("reporter_display_name = %q, want Alice", got[0].ReporterDisplayName)
	}
	if got[1].ReporterDisplayName != "" {
		t.Errorf("reporter_display_name = %q for an unnamed member, want empty", got[1].ReporterDisplayName)
	}
}

// The audit trail names both parties, and both listings join the same way.
func TestModerationActionRepo_ListActions_NamesBothParties(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewModerationActionRepo(postgres.New(pool))

	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("aname-target"), domain.RoleMember, 50)
	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("aname-mod"), domain.RoleModerator, 80)
	nameUser(t, pool, target.ID, "Alice")
	nameUser(t, pool, moderator.ID, "Mallory")

	newModerationAction(t, pool, target.ID, moderator.ID, time.Now())

	byTarget, err := repo.ListActionsByTarget(ctx, target.ID, 10, 0)
	if err != nil {
		t.Fatalf("ListActionsByTarget: %v", err)
	}
	if len(byTarget) != 1 {
		t.Fatalf("%d actions by target, want 1", len(byTarget))
	}
	if byTarget[0].TargetDisplayName != "Alice" || byTarget[0].ModeratorDisplayName != "Mallory" {
		t.Errorf("by target names = %q/%q, want Alice/Mallory",
			byTarget[0].TargetDisplayName, byTarget[0].ModeratorDisplayName)
	}

	byModerator, err := repo.ListActionsByModerator(ctx, moderator.ID, 10, 0)
	if err != nil {
		t.Fatalf("ListActionsByModerator: %v", err)
	}
	if len(byModerator) != 1 {
		t.Fatalf("%d actions by moderator, want 1", len(byModerator))
	}
	if byModerator[0].TargetDisplayName != "Alice" || byModerator[0].ModeratorDisplayName != "Mallory" {
		t.Errorf("by moderator names = %q/%q, want Alice/Mallory",
			byModerator[0].TargetDisplayName, byModerator[0].ModeratorDisplayName)
	}
}

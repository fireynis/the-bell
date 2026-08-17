//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The invitation rules that live in the schema rather than in Go — the
// one-live-invite-per-address index, the exactly-once consume, the liveness
// clause every lookup repeats — can only be checked against a real Postgres.
// That is the whole reason these tests exist rather than more service-level
// ones: the fake in invite_test.go is a re-implementation of these rules, and
// something has to establish that the re-implementation matches.

var inviteSeq int

func uniqueInviteEmail(prefix string) string {
	inviteSeq++
	return fmt.Sprintf("%s-%d-%d@example.com", prefix, time.Now().UnixNano(), inviteSeq)
}

func newInviteRepo(t *testing.T, pool *pgxpool.Pool) *postgres.InviteRepo {
	t.Helper()
	return postgres.NewInviteRepo(postgres.New(pool))
}

func seedInvite(t *testing.T, repo *postgres.InviteRepo, inviter *domain.User, email string, created, expires time.Time) (*domain.Invite, string) {
	t.Helper()

	inviteSeq++
	invite := &domain.Invite{
		ID:        fmt.Sprintf("invite-%d-%d", time.Now().UnixNano(), inviteSeq),
		Email:     email,
		Note:      "seeded by a test",
		InviterID: inviter.ID,
		CreatedAt: created,
		ExpiresAt: expires,
	}
	hash := fmt.Sprintf("hash-%s", invite.ID)
	if err := repo.CreateInvite(context.Background(), invite, hash); err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	return invite, hash
}

func inviteFixtures(t *testing.T) (*pgxpool.Pool, *postgres.InviteRepo, *domain.User) {
	t.Helper()
	pool := testsupport.TestDB(t)
	repo := newInviteRepo(t, pool)
	inviter := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("inviter"), domain.RoleMember, 80)
	return pool, repo, inviter
}

func TestInviteRepo_CreateAndLookUpByToken(t *testing.T) {
	_, repo, inviter := inviteFixtures(t)
	ctx := context.Background()
	now := time.Now()

	email := uniqueInviteEmail("newcomer")
	invite, hash := seedInvite(t, repo, inviter, email, now, now.Add(14*24*time.Hour))

	got, err := repo.GetLiveInviteByTokenHash(ctx, hash, now)
	if err != nil {
		t.Fatalf("GetLiveInviteByTokenHash: %v", err)
	}
	if got.ID != invite.ID {
		t.Errorf("id = %q, want %q", got.ID, invite.ID)
	}
	if got.Email != email {
		t.Errorf("email = %q, want %q", got.Email, email)
	}
	// Joined in by the query, because the greeting the invitee sees names the
	// person who invited them.
	if got.InviterDisplayName != inviter.DisplayName {
		t.Errorf("inviter_display_name = %q, want %q", got.InviterDisplayName, inviter.DisplayName)
	}
}

// One unconsumed, unrevoked invitation per address, enforced by the index
// rather than by the service — which is what makes it safe against two
// simultaneous requests, where both pass the service's check.
func TestInviteRepo_CreateRefusesASecondLiveInvitationForOneAddress(t *testing.T) {
	pool, repo, inviter := inviteFixtures(t)
	ctx := context.Background()
	now := time.Now()

	email := uniqueInviteEmail("contested")
	seedInvite(t, repo, inviter, email, now, now.Add(14*24*time.Hour))

	other := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("other-inviter"), domain.RoleMember, 80)
	second := &domain.Invite{
		ID: "invite-second", Email: email, InviterID: other.ID,
		CreatedAt: now, ExpiresAt: now.Add(14 * 24 * time.Hour),
	}

	err := repo.CreateInvite(ctx, second, "hash-second")
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("CreateInvite error = %v, want ErrValidation from the unique index", err)
	}
}

// The index is on lower(email), so capitalisation cannot buy a second live
// invitation for the same person.
func TestInviteRepo_TheLiveInviteRuleIsCaseInsensitive(t *testing.T) {
	_, repo, inviter := inviteFixtures(t)
	ctx := context.Background()
	now := time.Now()

	email := uniqueInviteEmail("mixedcase")
	seedInvite(t, repo, inviter, email, now, now.Add(14*24*time.Hour))

	shouted := &domain.Invite{
		ID: "invite-shouted", Email: upper(email), InviterID: inviter.ID,
		CreatedAt: now, ExpiresAt: now.Add(14 * 24 * time.Hour),
	}
	if err := repo.CreateInvite(ctx, shouted, "hash-shouted"); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("CreateInvite error = %v, want ErrValidation", err)
	}

	// And the lookups match the same way.
	got, err := repo.GetLiveInviteByEmail(ctx, upper(email), now)
	if err != nil {
		t.Fatalf("GetLiveInviteByEmail with a capitalised address: %v", err)
	}
	if got.Email != email {
		t.Errorf("email = %q, want the address as it was stored (%q)", got.Email, email)
	}
}

func upper(s string) string {
	out := []rune(s)
	for i, r := range out {
		if r >= 'a' && r <= 'z' {
			out[i] = r - 32
		}
	}
	return string(out)
}

// The unique index cannot test expiry — an index predicate must be immutable —
// so an expired invitation still holds its address until the service reaps it.
// That reap is what makes a mistyped address recoverable rather than burned
// forever, and it is the reason GetBlockingInviteByEmail exists alongside the
// live lookup.
func TestInviteRepo_AnExpiredInvitationStillBlocksUntilReaped(t *testing.T) {
	_, repo, inviter := inviteFixtures(t)
	ctx := context.Background()
	now := time.Now()

	email := uniqueInviteEmail("expired")
	expired, _ := seedInvite(t, repo, inviter, email, now.Add(-30*24*time.Hour), now.Add(-time.Hour))

	// Not live...
	if _, err := repo.GetLiveInviteByEmail(ctx, email, now); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("GetLiveInviteByEmail on an expired invitation = %v, want ErrNotFound", err)
	}
	// ...but still occupying the address.
	blocking, err := repo.GetBlockingInviteByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetBlockingInviteByEmail: %v", err)
	}
	if blocking.ID != expired.ID {
		t.Fatalf("blocking invitation = %q, want %q", blocking.ID, expired.ID)
	}
	replacement := &domain.Invite{
		ID: "invite-replacement", Email: email, InviterID: inviter.ID,
		CreatedAt: now, ExpiresAt: now.Add(14 * 24 * time.Hour),
	}
	if err := repo.CreateInvite(ctx, replacement, "hash-replacement"); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("CreateInvite before the reap = %v, want the index to refuse it", err)
	}

	// Reaped, the address is free again — and the reaped row still reads as
	// expired rather than as one the inviter withdrew.
	if err := repo.ReapInvite(ctx, expired.ID, now); err != nil {
		t.Fatalf("ReapInvite: %v", err)
	}
	if err := repo.CreateInvite(ctx, replacement, "hash-replacement"); err != nil {
		t.Fatalf("CreateInvite after the reap: %v", err)
	}

	reaped := findInvite(t, repo, inviter.ID, expired.ID)
	if got := reaped.Status(now); got != domain.InviteExpired {
		t.Errorf("reaped invitation status = %q, want %q", got, domain.InviteExpired)
	}
}

func findInvite(t *testing.T, repo *postgres.InviteRepo, inviterID, inviteID string) *domain.Invite {
	t.Helper()
	invites, err := repo.ListInvitesByInviter(context.Background(), inviterID)
	if err != nil {
		t.Fatalf("ListInvitesByInviter: %v", err)
	}
	for _, invite := range invites {
		if invite.ID == inviteID {
			return invite
		}
	}
	t.Fatalf("invitation %q is not in the inviter's list", inviteID)
	return nil
}

// The conditional UPDATE is the exactly-once guard: two sign-ins with the same
// address both find the invitation live, and only one of them updates a row.
func TestInviteRepo_ConsumeSucceedsExactlyOnce(t *testing.T) {
	pool, repo, inviter := inviteFixtures(t)
	ctx := context.Background()
	now := time.Now()

	email := uniqueInviteEmail("consumed")
	invite, _ := seedInvite(t, repo, inviter, email, now, now.Add(14*24*time.Hour))
	first := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("first"), domain.RolePending, 50)
	second := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("second"), domain.RolePending, 50)

	got, err := repo.ConsumeInvite(ctx, invite.ID, first.ID, now)
	if err != nil {
		t.Fatalf("first ConsumeInvite: %v", err)
	}
	if got.ConsumedBy != first.ID {
		t.Errorf("consumed_by = %q, want %q", got.ConsumedBy, first.ID)
	}

	if _, err := repo.ConsumeInvite(ctx, invite.ID, second.ID, now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second ConsumeInvite = %v, want ErrNotFound", err)
	}

	after := findInvite(t, repo, inviter.ID, invite.ID)
	if after.ConsumedBy != first.ID {
		t.Errorf("consumed_by = %q after the second attempt, want it unchanged at %q", after.ConsumedBy, first.ID)
	}
	if after.Status(now) != domain.InviteAccepted {
		t.Errorf("status = %q, want accepted", after.Status(now))
	}
}

func TestInviteRepo_ConsumeRefusesAnInvitationThatIsNoLongerLive(t *testing.T) {
	pool, repo, inviter := inviteFixtures(t)
	ctx := context.Background()
	now := time.Now()
	newcomer := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("newcomer"), domain.RolePending, 50)

	t.Run("expired", func(t *testing.T) {
		invite, _ := seedInvite(t, repo, inviter, uniqueInviteEmail("stale"), now.Add(-30*24*time.Hour), now.Add(-time.Hour))
		if _, err := repo.ConsumeInvite(ctx, invite.ID, newcomer.ID, now); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("ConsumeInvite on an expired invitation = %v, want ErrNotFound", err)
		}
	})

	t.Run("revoked", func(t *testing.T) {
		invite, _ := seedInvite(t, repo, inviter, uniqueInviteEmail("withdrawn"), now, now.Add(14*24*time.Hour))
		if err := repo.RevokeInvite(ctx, invite.ID, inviter.ID, now); err != nil {
			t.Fatalf("RevokeInvite: %v", err)
		}
		if _, err := repo.ConsumeInvite(ctx, invite.ID, newcomer.ID, now); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("ConsumeInvite on a revoked invitation = %v, want ErrNotFound", err)
		}
	})
}

// Ownership is part of the UPDATE's WHERE clause, so somebody else's
// invitation, an accepted one and an id that names nothing are indistinguishable
// from outside.
func TestInviteRepo_RevokeIsScopedToTheInviter(t *testing.T) {
	pool, repo, inviter := inviteFixtures(t)
	ctx := context.Background()
	now := time.Now()

	invite, _ := seedInvite(t, repo, inviter, uniqueInviteEmail("mine"), now, now.Add(14*24*time.Hour))
	stranger := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("stranger"), domain.RoleMember, 80)

	if err := repo.RevokeInvite(ctx, invite.ID, stranger.ID, now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("RevokeInvite by a stranger = %v, want ErrNotFound", err)
	}
	if got := findInvite(t, repo, inviter.ID, invite.ID); got.RevokedAt != nil {
		t.Error("a stranger's revocation took effect")
	}

	if err := repo.RevokeInvite(ctx, invite.ID, inviter.ID, now); err != nil {
		t.Fatalf("RevokeInvite by the inviter: %v", err)
	}
	if got := findInvite(t, repo, inviter.ID, invite.ID).Status(now); got != domain.InviteRevoked {
		t.Errorf("status = %q, want %q", got, domain.InviteRevoked)
	}

	// Revoking twice is the same non-answer as revoking somebody else's.
	if err := repo.RevokeInvite(ctx, invite.ID, inviter.ID, now); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second RevokeInvite = %v, want ErrNotFound", err)
	}
}

func TestInviteRepo_ListIsTheInvitersOwnNewestFirst(t *testing.T) {
	pool, repo, inviter := inviteFixtures(t)
	ctx := context.Background()
	now := time.Now()

	older, _ := seedInvite(t, repo, inviter, uniqueInviteEmail("older"), now.Add(-2*time.Hour), now.Add(14*24*time.Hour))
	newer, _ := seedInvite(t, repo, inviter, uniqueInviteEmail("newer"), now.Add(-time.Hour), now.Add(14*24*time.Hour))

	stranger := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("stranger-lister"), domain.RoleMember, 80)
	seedInvite(t, repo, stranger, uniqueInviteEmail("theirs"), now, now.Add(14*24*time.Hour))

	invites, err := repo.ListInvitesByInviter(ctx, inviter.ID)
	if err != nil {
		t.Fatalf("ListInvitesByInviter: %v", err)
	}
	if len(invites) != 2 {
		t.Fatalf("listed %d invitations, want 2 (somebody else's must not appear)", len(invites))
	}
	if invites[0].ID != newer.ID || invites[1].ID != older.ID {
		t.Errorf("order = %q, %q; want newest first (%q, %q)",
			invites[0].ID, invites[1].ID, newer.ID, older.ID)
	}
}

func TestInviteRepo_ListNamesWhoeverAccepted(t *testing.T) {
	pool, repo, inviter := inviteFixtures(t)
	ctx := context.Background()
	now := time.Now()

	invite, _ := seedInvite(t, repo, inviter, uniqueInviteEmail("accepted"), now, now.Add(14*24*time.Hour))
	newcomer := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("acceptor"), domain.RolePending, 50)
	if _, err := repo.ConsumeInvite(ctx, invite.ID, newcomer.ID, now); err != nil {
		t.Fatalf("ConsumeInvite: %v", err)
	}

	got := findInvite(t, repo, inviter.ID, invite.ID)
	if got.ConsumedByDisplayName != newcomer.DisplayName {
		t.Errorf("consumed_by_display_name = %q, want %q", got.ConsumedByDisplayName, newcomer.DisplayName)
	}
}

func TestInviteRepo_CountsForTheDailyBudget(t *testing.T) {
	_, repo, inviter := inviteFixtures(t)
	ctx := context.Background()
	now := time.Now()
	since := now.Add(-12 * time.Hour)

	seedInvite(t, repo, inviter, uniqueInviteEmail("today-a"), now.Add(-time.Hour), now.Add(14*24*time.Hour))
	seedInvite(t, repo, inviter, uniqueInviteEmail("today-b"), now.Add(-2*time.Hour), now.Add(14*24*time.Hour))
	seedInvite(t, repo, inviter, uniqueInviteEmail("yesterday"), now.Add(-30*time.Hour), now.Add(14*24*time.Hour))

	count, err := repo.CountInvitesByInviterSince(ctx, inviter.ID, since)
	if err != nil {
		t.Fatalf("CountInvitesByInviterSince: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2 — only invitations created inside the window", count)
	}
}

func TestInviteRepo_CountsAcceptedInvitationsPerAddress(t *testing.T) {
	pool, repo, inviter := inviteFixtures(t)
	ctx := context.Background()
	now := time.Now()

	email := uniqueInviteEmail("returning")
	invite, _ := seedInvite(t, repo, inviter, email, now, now.Add(14*24*time.Hour))

	count, err := repo.CountConsumedInvitesByEmail(ctx, email)
	if err != nil {
		t.Fatalf("CountConsumedInvitesByEmail: %v", err)
	}
	if count != 0 {
		t.Fatalf("count before acceptance = %d, want 0", count)
	}

	newcomer := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("returner"), domain.RolePending, 50)
	if _, err := repo.ConsumeInvite(ctx, invite.ID, newcomer.ID, now); err != nil {
		t.Fatalf("ConsumeInvite: %v", err)
	}

	// Matched case-insensitively, like every other read of this column.
	count, err = repo.CountConsumedInvitesByEmail(ctx, upper(email))
	if err != nil {
		t.Fatalf("CountConsumedInvitesByEmail: %v", err)
	}
	if count != 1 {
		t.Errorf("count after acceptance = %d, want 1", count)
	}
}

// Migration 00023 seeds the mode so that a town upgrading into this feature is
// invite-only rather than silently still open.
func TestMigration_SeedsInviteRegistrationMode(t *testing.T) {
	pool := testsupport.TestDB(t)

	var mode string
	err := pool.QueryRow(context.Background(),
		`SELECT value FROM town_config WHERE key = 'registration_mode'`).Scan(&mode)
	if err != nil {
		t.Fatalf("reading registration_mode: %v", err)
	}
	if mode != "invite" {
		t.Errorf("registration_mode = %q, want invite", mode)
	}
}

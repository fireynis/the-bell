//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
)

// namedUser creates a directory fixture: a user with a display name, since the
// search is over names and testsupport.TestUser leaves them blank.
func namedUser(t *testing.T, pool *pgxpool.Pool, suffix, name string, role domain.Role) *domain.User {
	t.Helper()

	repo := postgres.NewUserRepo(postgres.New(pool))
	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID(suffix), role, 50)
	updated, err := repo.UpdateUserProfile(context.Background(), user.ID, name, "", "")
	if err != nil {
		t.Fatalf("UpdateUserProfile(%s): %v", name, err)
	}
	return updated
}

// directoryIDs collects the ids on a page, for membership assertions that do
// not depend on order.
func directoryIDs(users []*domain.User) map[string]bool {
	ids := make(map[string]bool, len(users))
	for _, u := range users {
		ids[u.ID] = true
	}
	return ids
}

// Who the directory shows. Pending residents are in it deliberately — nobody
// can vouch for someone they cannot find — and the three kinds of account that
// must not appear are excluded by two different mechanisms, so each is checked.
func TestUserRepo_ListDirectoryUsers_Excludes(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewUserRepo(postgres.New(pool))

	pending := namedUser(t, pool, "dir-pending", "Pat Pending", domain.RolePending)
	member := namedUser(t, pool, "dir-member", "Mel Member", domain.RoleMember)
	moderator := namedUser(t, pool, "dir-mod", "Mo Moderator", domain.RoleModerator)
	council := namedUser(t, pool, "dir-council", "Cass Council", domain.RoleCouncil)

	banned := namedUser(t, pool, "dir-banned", "Mallory Banned", domain.RoleBanned)

	deactivated := namedUser(t, pool, "dir-gone", "Dee Departed", domain.RoleMember)
	if err := repo.DeactivateUser(ctx, deactivated.ID); err != nil {
		t.Fatalf("DeactivateUser: %v", err)
	}

	suspended := namedUser(t, pool, "dir-suspended", "Sam Suspended", domain.RoleMember)
	until := time.Now().Add(24 * time.Hour)
	if err := repo.SetUserSuspendedUntil(ctx, suspended.ID, &until); err != nil {
		t.Fatalf("SetUserSuspendedUntil: %v", err)
	}

	// A suspension that has already run its course is not a suspension. The
	// clock is part of the filter, so this resident must be back in the list.
	lapsed := namedUser(t, pool, "dir-lapsed", "Lee Lapsed", domain.RoleMember)
	past := time.Now().Add(-time.Hour)
	if err := repo.SetUserSuspendedUntil(ctx, lapsed.ID, &past); err != nil {
		t.Fatalf("SetUserSuspendedUntil: %v", err)
	}

	got, err := repo.ListDirectoryUsers(ctx, "", 100, 0)
	if err != nil {
		t.Fatalf("ListDirectoryUsers: %v", err)
	}
	ids := directoryIDs(got)

	for _, want := range []*domain.User{pending, member, moderator, council, lapsed} {
		if !ids[want.ID] {
			t.Errorf("%s (%s) is missing from the directory", want.DisplayName, want.Role)
		}
	}
	for name, excluded := range map[string]*domain.User{
		"banned":              banned,
		"deactivated":         deactivated,
		"currently suspended": suspended,
	} {
		if ids[excluded.ID] {
			t.Errorf("a %s account (%s) is in the directory", name, excluded.DisplayName)
		}
	}

	total, err := repo.CountDirectoryUsers(ctx, "")
	if err != nil {
		t.Fatalf("CountDirectoryUsers: %v", err)
	}
	if total != int64(len(got)) {
		t.Errorf("total = %d but the page holds %d; the two filters have diverged", total, len(got))
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
}

// The search is a case-insensitive substring of the display name — not a
// prefix, and not case-sensitive, because a resident hunting for a neighbour
// types what they remember rather than what was entered.
func TestUserRepo_ListDirectoryUsers_SubstringSearch(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewUserRepo(postgres.New(pool))

	alice := namedUser(t, pool, "dir-alice", "Alice Anderson", domain.RoleMember)
	alistair := namedUser(t, pool, "dir-alistair", "Alistair Brown", domain.RolePending)
	bob := namedUser(t, pool, "dir-bob", "Bob Carter", domain.RoleMember)
	// A name with LIKE's own metacharacters in it. Unescaped, a search for "_"
	// or "%" would match every resident instead of this one.
	odd := namedUser(t, pool, "dir-odd", "Percent%Under_score", domain.RoleMember)

	tests := []struct {
		name  string
		query string
		want  []*domain.User
	}{
		{"empty lists everyone", "", []*domain.User{alice, alistair, bob, odd}},
		{"a shared prefix", "ali", []*domain.User{alice, alistair}},
		{"case is ignored", "ALI", []*domain.User{alice, alistair}},
		{"a substring in the middle", "nderso", []*domain.User{alice}},
		{"a surname", "carter", []*domain.User{bob}},
		{"no match is empty, not an error", "zzzz", nil},
		// The escaping cases. Each of these is a literal character in one name,
		// and LIKE syntax that would otherwise match everybody.
		{"a percent sign is literal", "%", []*domain.User{odd}},
		{"an underscore is literal", "_", []*domain.User{odd}},
		{"a backslash is literal", `\`, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repo.ListDirectoryUsers(ctx, tt.query, 100, 0)
			if err != nil {
				t.Fatalf("ListDirectoryUsers(%q): %v", tt.query, err)
			}
			ids := directoryIDs(got)

			if len(got) != len(tt.want) {
				t.Errorf("%d matches for %q, want %d", len(got), tt.query, len(tt.want))
			}
			for _, want := range tt.want {
				if !ids[want.ID] {
					t.Errorf("%q did not match %q", tt.query, want.DisplayName)
				}
			}

			total, err := repo.CountDirectoryUsers(ctx, tt.query)
			if err != nil {
				t.Fatalf("CountDirectoryUsers(%q): %v", tt.query, err)
			}
			if total != int64(len(tt.want)) {
				t.Errorf("total for %q = %d, want %d", tt.query, total, len(tt.want))
			}
		})
	}
}

// Newest first, and stable across pages. Offset pagination over an ambiguous
// order silently drops and repeats rows between pages.
func TestUserRepo_ListDirectoryUsers_OrderAndPaging(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewUserRepo(postgres.New(pool))

	// Created oldest first, so the expected order is the reverse of this.
	var created []*domain.User
	for _, name := range []string{"First Arrival", "Second Arrival", "Third Arrival", "Fourth Arrival", "Fifth Arrival"} {
		created = append(created, namedUser(t, pool, "dir-order-"+name, name, domain.RoleMember))
	}

	var walked []string
	for offset := 0; offset < len(created); offset += 2 {
		page, err := repo.ListDirectoryUsers(ctx, "Arrival", 2, offset)
		if err != nil {
			t.Fatalf("ListDirectoryUsers(offset=%d): %v", offset, err)
		}
		for _, u := range page {
			walked = append(walked, u.ID)
		}
	}

	if len(walked) != len(created) {
		t.Fatalf("walking the pages yielded %d users, want %d", len(walked), len(created))
	}
	for i, id := range walked {
		want := created[len(created)-1-i]
		if id != want.ID {
			t.Errorf("position %d is %s, want %s (%s) — newest first", i, id, want.ID, want.DisplayName)
		}
	}

	// An offset past the end is an empty page, not an error and not a wrap.
	beyond, err := repo.ListDirectoryUsers(ctx, "Arrival", 2, 500)
	if err != nil {
		t.Fatalf("ListDirectoryUsers(offset=500): %v", err)
	}
	if len(beyond) != 0 {
		t.Errorf("offset past the end returned %d users, want none", len(beyond))
	}
}

// The fields the directory renders have to survive the round trip. A zero
// JoinedAt would make every neighbour look like they arrived in year one.
func TestUserRepo_ListDirectoryUsers_PopulatesRenderedFields(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewUserRepo(postgres.New(pool))

	created := namedUser(t, pool, "dir-fields", "Ada Lovelace", domain.RoleModerator)

	got, err := repo.ListDirectoryUsers(ctx, "Ada", 10, 0)
	if err != nil {
		t.Fatalf("ListDirectoryUsers: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("%d matches, want 1", len(got))
	}

	user := got[0]
	if user.ID != created.ID {
		t.Errorf("id = %q, want %q", user.ID, created.ID)
	}
	if user.DisplayName != "Ada Lovelace" {
		t.Errorf("display_name = %q, want %q", user.DisplayName, "Ada Lovelace")
	}
	if user.Role != domain.RoleModerator {
		t.Errorf("role = %q, want %q", user.Role, domain.RoleModerator)
	}
	if user.JoinedAt.IsZero() {
		t.Error("joined_at is zero")
	}
}

// A resident who never set a name is still in the directory. They are the ones
// most in need of being found — a blank name is what a brand-new pending
// account looks like — so an empty display name must not filter them out.
func TestUserRepo_ListDirectoryUsers_IncludesUnnamedResidents(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewUserRepo(postgres.New(pool))

	unnamed := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("dir-unnamed"), domain.RolePending, 50)

	got, err := repo.ListDirectoryUsers(ctx, "", 100, 0)
	if err != nil {
		t.Fatalf("ListDirectoryUsers: %v", err)
	}
	if !directoryIDs(got)[unnamed.ID] {
		t.Error("a resident with no display name is missing from the directory")
	}

	// They are not, however, reachable by a name search — there is no name to
	// match, and an empty query is the listing that finds them.
	filtered, err := repo.ListDirectoryUsers(ctx, "anything", 100, 0)
	if err != nil {
		t.Fatalf("ListDirectoryUsers: %v", err)
	}
	if directoryIDs(filtered)[unnamed.ID] {
		t.Error("an unnamed resident matched a name search")
	}
}

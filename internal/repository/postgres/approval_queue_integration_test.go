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

// waitingSince creates a pending applicant with a display name and a fixed
// join date.
//
// The date is written directly because nothing in the repository sets it: it is
// stamped at account creation, and every fixture made in one test would
// otherwise be seconds apart in an order the test could not state. The queue is
// ordered by exactly this column, so pinning it is what makes the assertions
// about ordering mean anything.
func waitingSince(t *testing.T, pool *pgxpool.Pool, suffix, name string, joined time.Time) *domain.User {
	t.Helper()

	user := namedUser(t, pool, suffix, name, domain.RolePending)
	if _, err := pool.Exec(context.Background(),
		"UPDATE users SET joined_at = $2 WHERE id = $1", user.ID, joined); err != nil {
		t.Fatalf("setting joined_at for %s: %v", name, err)
	}
	user.JoinedAt = joined
	return user
}

// Who the queue shows. It is the council's list of applications to decide, so
// it holds pending accounts and nothing else — and the same three kinds of
// account the directory excludes are excluded here too, since none of them has
// an application to review.
func TestUserRepo_ListPendingUsersPage_Excludes(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewUserRepo(postgres.New(pool))

	base := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	waiting := waitingSince(t, pool, "queue-waiting", "Pat Pending", base)

	member := namedUser(t, pool, "queue-member", "Mel Member", domain.RoleMember)
	council := namedUser(t, pool, "queue-council", "Cass Council", domain.RoleCouncil)
	banned := namedUser(t, pool, "queue-banned", "Mallory Banned", domain.RoleBanned)

	deactivated := waitingSince(t, pool, "queue-gone", "Dee Departed", base)
	if err := repo.DeactivateUser(ctx, deactivated.ID); err != nil {
		t.Fatalf("DeactivateUser: %v", err)
	}

	suspended := waitingSince(t, pool, "queue-suspended", "Sam Suspended", base)
	until := time.Now().Add(24 * time.Hour)
	if err := repo.SetUserSuspendedUntil(ctx, suspended.ID, &until); err != nil {
		t.Fatalf("SetUserSuspendedUntil: %v", err)
	}

	// A suspension that has run its course is not a suspension, so this
	// applicant is reviewable again the moment it lapses.
	lapsed := waitingSince(t, pool, "queue-lapsed", "Lee Lapsed", base)
	past := time.Now().Add(-time.Hour)
	if err := repo.SetUserSuspendedUntil(ctx, lapsed.ID, &past); err != nil {
		t.Fatalf("SetUserSuspendedUntil: %v", err)
	}

	got, err := repo.ListPendingUsersPage(ctx, "", 100, 0)
	if err != nil {
		t.Fatalf("ListPendingUsersPage: %v", err)
	}
	ids := directoryIDs(got)

	for _, want := range []*domain.User{waiting, lapsed} {
		if !ids[want.ID] {
			t.Errorf("%s is missing from the approval queue", want.DisplayName)
		}
	}
	for name, excluded := range map[string]*domain.User{
		"member":              member,
		"council":             council,
		"banned":              banned,
		"deactivated":         deactivated,
		"currently suspended": suspended,
	} {
		if ids[excluded.ID] {
			t.Errorf("a %s account (%s) is in the approval queue", name, excluded.DisplayName)
		}
	}

	total, err := repo.CountPendingUsersMatching(ctx, "")
	if err != nil {
		t.Fatalf("CountPendingUsersMatching: %v", err)
	}
	if total != int64(len(got)) {
		t.Errorf("total = %d but the page holds %d; the two filters have diverged", total, len(got))
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
}

// Oldest first, and stable across pages. This is the opposite of the directory
// and it is the point of the queue: the applicant who has waited longest is
// reviewed first, so a registration flood cannot bury them.
func TestUserRepo_ListPendingUsersPage_OldestFirstAndPages(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewUserRepo(postgres.New(pool))

	base := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	names := []string{"First Waiting", "Second Waiting", "Third Waiting", "Fourth Waiting", "Fifth Waiting"}

	// Created in the order they should come back, a day apart.
	var arrived []*domain.User
	for i, name := range names {
		arrived = append(arrived, waitingSince(t, pool, "queue-order-"+name, name, base.AddDate(0, 0, i)))
	}

	var walked []string
	for offset := 0; offset < len(arrived); offset += 2 {
		page, err := repo.ListPendingUsersPage(ctx, "Waiting", 2, offset)
		if err != nil {
			t.Fatalf("ListPendingUsersPage(offset=%d): %v", offset, err)
		}
		for _, u := range page {
			walked = append(walked, u.ID)
		}
	}

	if len(walked) != len(arrived) {
		t.Fatalf("walking the pages yielded %d applicants, want %d", len(walked), len(arrived))
	}
	for i, id := range walked {
		if id != arrived[i].ID {
			t.Errorf("position %d is %s, want %s (%s) — oldest first",
				i, id, arrived[i].ID, arrived[i].DisplayName)
		}
	}

	// An offset past the end is an empty page, not an error and not a wrap.
	beyond, err := repo.ListPendingUsersPage(ctx, "Waiting", 2, 500)
	if err != nil {
		t.Fatalf("ListPendingUsersPage(offset=500): %v", err)
	}
	if len(beyond) != 0 {
		t.Errorf("offset past the end returned %d applicants, want none", len(beyond))
	}
}

// Applicants who joined in the same instant still page cleanly: the id tiebreak
// is what stops offset paging repeating one and skipping another.
func TestUserRepo_ListPendingUsersPage_TiedJoinDatesPageWithoutOverlap(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewUserRepo(postgres.New(pool))

	same := time.Date(2026, 4, 15, 8, 30, 0, 0, time.UTC)
	for _, name := range []string{"Tied Alpha", "Tied Bravo", "Tied Charlie", "Tied Delta"} {
		waitingSince(t, pool, "queue-tied-"+name, name, same)
	}

	seen := map[string]bool{}
	for offset := 0; offset < 4; offset += 2 {
		page, err := repo.ListPendingUsersPage(ctx, "Tied", 2, offset)
		if err != nil {
			t.Fatalf("ListPendingUsersPage(offset=%d): %v", offset, err)
		}
		for _, u := range page {
			if seen[u.ID] {
				t.Errorf("%s appeared on two pages", u.DisplayName)
			}
			seen[u.ID] = true
		}
	}
	if len(seen) != 4 {
		t.Errorf("walking the pages saw %d of 4 applicants", len(seen))
	}
}

// The search is the directory's: a case-insensitive substring of the display
// name, with LIKE's own metacharacters escaped so a council member searching
// for "_" finds an underscore rather than everybody.
func TestUserRepo_ListPendingUsersPage_SubstringSearch(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewUserRepo(postgres.New(pool))

	base := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	alice := waitingSince(t, pool, "queue-alice", "Alice Anderson", base)
	alistair := waitingSince(t, pool, "queue-alistair", "Alistair Brown", base.AddDate(0, 0, 1))
	bob := waitingSince(t, pool, "queue-bob", "Bob Carter", base.AddDate(0, 0, 2))
	odd := waitingSince(t, pool, "queue-odd", "Percent%Under_score", base.AddDate(0, 0, 3))

	// A member with a matching name, to prove the search narrows the queue
	// rather than reaching outside it.
	namedUser(t, pool, "queue-alma", "Alma Member", domain.RoleMember)

	tests := []struct {
		name  string
		query string
		want  []*domain.User
	}{
		{"empty lists everyone waiting", "", []*domain.User{alice, alistair, bob, odd}},
		{"a shared prefix", "ali", []*domain.User{alice, alistair}},
		{"case is ignored", "ALI", []*domain.User{alice, alistair}},
		{"a substring in the middle", "nderso", []*domain.User{alice}},
		{"no match is empty, not an error", "zzzz", nil},
		{"a percent sign is literal", "%", []*domain.User{odd}},
		{"an underscore is literal", "_", []*domain.User{odd}},
		{"a backslash is literal", `\`, nil},
		{"a member is not reachable from the queue", "Alma", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repo.ListPendingUsersPage(ctx, tt.query, 100, 0)
			if err != nil {
				t.Fatalf("ListPendingUsersPage(%q): %v", tt.query, err)
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

			total, err := repo.CountPendingUsersMatching(ctx, tt.query)
			if err != nil {
				t.Fatalf("CountPendingUsersMatching(%q): %v", tt.query, err)
			}
			if total != int64(len(tt.want)) {
				t.Errorf("total for %q = %d, want %d", tt.query, total, len(tt.want))
			}
		})
	}
}

// The queue is the one listing that carries a residency claim, so it has to
// survive the round trip: a page that dropped it would leave the council
// deciding on a name and a date alone.
func TestUserRepo_ListPendingUsersPage_PopulatesRenderedFields(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewUserRepo(postgres.New(pool))

	joined := time.Date(2026, 2, 14, 11, 0, 0, 0, time.UTC)
	created := waitingSince(t, pool, "queue-fields", "Ada Lovelace", joined)
	if err := repo.SetUserResidencyClaim(ctx, created.ID, "the blue house behind the old mill"); err != nil {
		t.Fatalf("SetUserResidencyClaim: %v", err)
	}

	got, err := repo.ListPendingUsersPage(ctx, "Ada", 10, 0)
	if err != nil {
		t.Fatalf("ListPendingUsersPage: %v", err)
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
	if user.Role != domain.RolePending {
		t.Errorf("role = %q, want %q", user.Role, domain.RolePending)
	}
	if !user.JoinedAt.Equal(joined) {
		t.Errorf("joined_at = %s, want %s", user.JoinedAt, joined)
	}
	if user.ResidencyClaim != "the blue house behind the old mill" {
		t.Errorf("residency_claim = %q, want the claim that was stored", user.ResidencyClaim)
	}
}

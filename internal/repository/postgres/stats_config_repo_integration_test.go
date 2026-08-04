//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/fireynis/the-bell/internal/testsupport"
)

// --- StatsRepo ---

// Every stat is a COUNT with a WHERE that lives only in SQL, so each one is
// checked against a database holding rows it must deliberately exclude.

func TestStatsRepo_Counts(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewStatsRepo(postgres.New(pool))
	users := postgres.NewUserRepo(postgres.New(pool))

	// An empty town reports zeroes rather than failing.
	for name, count := range map[string]func(context.Context) (int64, error){
		"CountAllUsers":     repo.CountAllUsers,
		"CountPostsToday":   repo.CountPostsToday,
		"CountModerators":   repo.CountModerators,
		"CountPendingUsers": repo.CountPendingUsers,
	} {
		got, err := count(ctx)
		if err != nil || got != 0 {
			t.Fatalf("%s on an empty town = %d, %v; want 0, nil", name, got, err)
		}
	}

	member := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("stats-member"), domain.RoleMember, 50)
	testsupport.TestUser(t, pool, testsupport.UniqueKratosID("stats-mod-1"), domain.RoleModerator, 88)
	testsupport.TestUser(t, pool, testsupport.UniqueKratosID("stats-mod-2"), domain.RoleModerator, 88)
	testsupport.TestUser(t, pool, testsupport.UniqueKratosID("stats-council"), domain.RoleCouncil, 95)
	testsupport.TestUser(t, pool, testsupport.UniqueKratosID("stats-pending-1"), domain.RolePending, 50)
	testsupport.TestUser(t, pool, testsupport.UniqueKratosID("stats-pending-2"), domain.RolePending, 50)

	// A departed account is excluded from every count.
	departed := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("stats-gone"), domain.RoleModerator, 88)
	if err := users.DeactivateUser(ctx, departed.ID); err != nil {
		t.Fatalf("DeactivateUser: %v", err)
	}

	tests := []struct {
		name  string
		count func(context.Context) (int64, error)
		want  int64
	}{
		{"CountAllUsers counts active users of every role", repo.CountAllUsers, 6},
		{"CountModerators excludes council and the deactivated moderator", repo.CountModerators, 2},
		{"CountPendingUsers", repo.CountPendingUsers, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.count(ctx)
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if got != tt.want {
				t.Errorf("count = %d, want %d", got, tt.want)
			}
		})
	}

	// CountPostsToday is bounded by CURRENT_DATE and by status.
	now := time.Now()
	newPost(t, pool, "stats-today-1", member.ID, domain.PostVisible, now)
	newPost(t, pool, "stats-today-2", member.ID, domain.PostVisible, now)
	newPost(t, pool, "stats-removed", member.ID, domain.PostRemovedByMod, now)
	newPost(t, pool, "stats-deleted", member.ID, domain.PostRemovedByAuthor, now)
	newPost(t, pool, "stats-yesterday", member.ID, domain.PostVisible, now.AddDate(0, 0, -1))

	got, err := repo.CountPostsToday(ctx)
	if err != nil {
		t.Fatalf("CountPostsToday: %v", err)
	}
	if got != 2 {
		t.Errorf("CountPostsToday = %d, want 2 visible posts from today", got)
	}
}

// --- ConfigRepo ---

// Migration 00009 seeds bootstrap_mode, so a freshly migrated town already has
// one config row. Tests that count rows have to account for it, and the server
// relies on it: "no rows at all" is never a state it has to interpret.
const seededConfigRows = 1

func TestConfigRepo_MigrationSeedsBootstrapMode(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewConfigRepo(postgres.New(pool))

	got, err := repo.GetTownConfig(ctx, "bootstrap_mode")
	if err != nil {
		t.Fatalf("GetTownConfig(bootstrap_mode): %v", err)
	}
	if got != "false" {
		t.Errorf("bootstrap_mode = %q, want %q on a fresh database", got, "false")
	}
}

func TestConfigRepo_SetGetList(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewConfigRepo(postgres.New(pool))

	if err := repo.SetTownConfig(ctx, "town_name", "Bellwether"); err != nil {
		t.Fatalf("SetTownConfig: %v", err)
	}
	if err := repo.SetTownConfig(ctx, "setup_complete", "true"); err != nil {
		t.Fatalf("SetTownConfig: %v", err)
	}

	got, err := repo.GetTownConfig(ctx, "town_name")
	if err != nil {
		t.Fatalf("GetTownConfig: %v", err)
	}
	if got != "Bellwether" {
		t.Errorf("town_name = %q, want %q", got, "Bellwether")
	}

	all, err := repo.ListTownConfig(ctx)
	if err != nil {
		t.Fatalf("ListTownConfig: %v", err)
	}
	if len(all) != 2+seededConfigRows {
		t.Errorf("ListTownConfig = %v, want the 2 written keys plus the seeded one", all)
	}
	if all["town_name"] != "Bellwether" || all["setup_complete"] != "true" {
		t.Errorf("ListTownConfig = %v, want both written keys", all)
	}
}

// Set is an upsert: the settings screen writes the same keys repeatedly, and a
// second write must replace the value rather than fail on the primary key.
func TestConfigRepo_SetTownConfig_Overwrites(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewConfigRepo(postgres.New(pool))

	if err := repo.SetTownConfig(ctx, "town_name", "Bellwether"); err != nil {
		t.Fatalf("SetTownConfig: %v", err)
	}
	if err := repo.SetTownConfig(ctx, "town_name", "Belltown"); err != nil {
		t.Fatalf("SetTownConfig (overwrite): %v", err)
	}

	got, err := repo.GetTownConfig(ctx, "town_name")
	if err != nil {
		t.Fatalf("GetTownConfig: %v", err)
	}
	if got != "Belltown" {
		t.Errorf("town_name = %q, want the overwritten %q", got, "Belltown")
	}

	all, err := repo.ListTownConfig(ctx)
	if err != nil {
		t.Fatalf("ListTownConfig: %v", err)
	}
	if len(all) != 1+seededConfigRows {
		t.Errorf("ListTownConfig = %v, want town_name written once, not twice", all)
	}
}

// An unset key is how the server decides setup has not run yet, so it must be
// ErrNotFound and not a raw pgx error.
func TestConfigRepo_GetTownConfig_Missing(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewConfigRepo(postgres.New(pool))

	got, err := repo.GetTownConfig(ctx, "never_set")
	if !errors.Is(err, service.ErrNotFound) {
		t.Errorf("err = %v, want service.ErrNotFound", err)
	}
	if got != "" {
		t.Errorf("value = %q, want empty", got)
	}
}

// ListTownConfig always hands back a non-nil map, so the settings screen can
// range over it without a nil check.
func TestConfigRepo_ListTownConfig_ReturnsNonNilMap(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewConfigRepo(postgres.New(pool))

	all, err := repo.ListTownConfig(ctx)
	if err != nil {
		t.Fatalf("ListTownConfig: %v", err)
	}
	if all == nil {
		t.Fatal("got nil map, want one the caller can range over")
	}
	if len(all) != seededConfigRows {
		t.Errorf("got %v, want only the seeded row before anything is written", all)
	}
}

// Empty values are legitimate — clearing the town's tagline stores "", which
// must round-trip rather than reading back as missing.
func TestConfigRepo_EmptyValueRoundTrips(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewConfigRepo(postgres.New(pool))

	if err := repo.SetTownConfig(ctx, "tagline", ""); err != nil {
		t.Fatalf("SetTownConfig: %v", err)
	}

	got, err := repo.GetTownConfig(ctx, "tagline")
	if err != nil {
		t.Fatalf("GetTownConfig: %v", err)
	}
	if got != "" {
		t.Errorf("tagline = %q, want an empty string", got)
	}
}

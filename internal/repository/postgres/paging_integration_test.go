//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SQL's LIMIT and OFFSET are int4, so every list adapter narrows the int it is
// handed down to int32. A value above math.MaxInt32 wraps negative, and
// Postgres rejects a negative LIMIT and a negative OFFSET outright — so the
// call came back as a database error rather than as a page.
//
// It is reachable from the API: handler.parseOffset accepts any non-negative
// int with no upper bound, so ?offset=2147483648 is enough to reach it. These
// tests pin the boundary at the repository, which is where the narrowing
// happens — the handler's cap is one caller's policy, not a property of the
// adapter, and the feed cache and the CLI call these adapters directly.

// beyondInt32 is the first value that does not survive the narrowing.
const beyondInt32 = math.MaxInt32 + 1

var moderationActionSeq int

func newModerationAction(t *testing.T, pool *pgxpool.Pool, targetID, moderatorID string, createdAt time.Time) *domain.ModerationAction {
	t.Helper()

	moderationActionSeq++
	action := &domain.ModerationAction{
		ID:           fmt.Sprintf("action-%d", moderationActionSeq),
		TargetUserID: targetID,
		ModeratorID:  moderatorID,
		Action:       domain.ActionWarn,
		Severity:     1,
		Reason:       "spam",
		CreatedAt:    createdAt,
	}
	if err := postgres.NewModerationActionRepo(postgres.New(pool)).CreateModerationAction(context.Background(), action); err != nil {
		t.Fatalf("creating moderation action %s: %v", action.ID, err)
	}
	return action
}

func TestPostRepo_ListPosts_LimitBeyondInt32(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewPostRepo(postgres.New(pool))

	author := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("page-author"), domain.RoleMember, 50)
	now := time.Now()
	newPost(t, pool, "page-post-1", author.ID, domain.PostVisible, now.Add(-2*time.Hour))
	newPost(t, pool, "page-post-2", author.ID, domain.PostVisible, now.Add(-1*time.Hour))

	// The first page and the cursor page are separate statements, so both
	// narrow the limit and both have to hold.
	first, err := repo.ListPosts(ctx, "", beyondInt32)
	if err != nil {
		t.Fatalf("ListPosts (first page, oversized limit): %v", err)
	}
	if len(first) != 2 {
		t.Errorf("got %d posts, want all 2", len(first))
	}

	cursored, err := repo.ListPosts(ctx, "zzz", beyondInt32)
	if err != nil {
		t.Fatalf("ListPosts (cursor page, oversized limit): %v", err)
	}
	if len(cursored) != 2 {
		t.Errorf("got %d posts before the cursor, want all 2", len(cursored))
	}
}

func TestPostRepo_ListPostsByAuthor_LimitBeyondInt32(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewPostRepo(postgres.New(pool))

	author := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("page-author"), domain.RoleMember, 50)
	now := time.Now()
	newPost(t, pool, "page-post-1", author.ID, domain.PostVisible, now.Add(-2*time.Hour))
	newPost(t, pool, "page-post-2", author.ID, domain.PostVisible, now.Add(-1*time.Hour))

	got, err := repo.ListPostsByAuthor(ctx, author.ID, beyondInt32)
	if err != nil {
		t.Fatalf("ListPostsByAuthor (oversized limit): %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d posts, want all 2", len(got))
	}
}

func TestReportRepo_ListPendingReports_PagingBeyondInt32(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewReportRepo(postgres.New(pool))

	author := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("page-author"), domain.RoleMember, 50)
	reporter := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("page-reporter"), domain.RoleMember, 50)
	now := time.Now()
	newPost(t, pool, "page-post-1", author.ID, domain.PostVisible, now)
	newPost(t, pool, "page-post-2", author.ID, domain.PostVisible, now)
	newReport(t, pool, reporter.ID, "page-post-1", "pending", now.Add(-2*time.Hour))
	newReport(t, pool, reporter.ID, "page-post-2", "pending", now.Add(-1*time.Hour))

	t.Run("oversized limit returns every row", func(t *testing.T) {
		got, err := repo.ListPendingReports(ctx, beyondInt32, 0)
		if err != nil {
			t.Fatalf("ListPendingReports: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("got %d reports, want all 2", len(got))
		}
	})

	// An oversized offset is a page past the end of any real queue, so the
	// answer is an empty page — not an error.
	t.Run("oversized offset returns an empty page", func(t *testing.T) {
		got, err := repo.ListPendingReports(ctx, 10, beyondInt32)
		if err != nil {
			t.Fatalf("ListPendingReports: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("got %d reports past the end, want 0", len(got))
		}
	})
}

func TestModerationActionRepo_ListActions_PagingBeyondInt32(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewModerationActionRepo(postgres.New(pool))

	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("page-target"), domain.RoleMember, 50)
	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("page-mod"), domain.RoleModerator, 80)
	now := time.Now()
	newModerationAction(t, pool, target.ID, moderator.ID, now.Add(-2*time.Hour))
	newModerationAction(t, pool, target.ID, moderator.ID, now.Add(-1*time.Hour))

	// Both listings are separate statements over the same narrowing, so both
	// are exercised.
	listings := map[string]func(context.Context, string, int, int) ([]*domain.ModerationAction, error){
		"by target":    repo.ListActionsByTarget,
		"by moderator": repo.ListActionsByModerator,
	}
	ids := map[string]string{"by target": target.ID, "by moderator": moderator.ID}

	for name, list := range listings {
		t.Run(name+", oversized limit returns every row", func(t *testing.T) {
			got, err := list(ctx, ids[name], beyondInt32, 0)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(got) != 2 {
				t.Errorf("got %d actions, want all 2", len(got))
			}
		})

		t.Run(name+", oversized offset returns an empty page", func(t *testing.T) {
			got, err := list(ctx, ids[name], 10, beyondInt32)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(got) != 0 {
				t.Errorf("got %d actions past the end, want 0", len(got))
			}
		})
	}
}

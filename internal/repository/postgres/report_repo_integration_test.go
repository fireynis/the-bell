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
	"github.com/fireynis/the-bell/internal/service"
	"github.com/fireynis/the-bell/internal/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
)

var reportSeq int

func newReport(t *testing.T, pool *pgxpool.Pool, reporterID, postID, status string, createdAt time.Time) *domain.Report {
	t.Helper()

	reportSeq++
	report := &domain.Report{
		ID:         fmt.Sprintf("report-%d", reportSeq),
		ReporterID: reporterID,
		PostID:     postID,
		Reason:     "spam",
		Status:     status,
		CreatedAt:  createdAt,
	}
	if err := postgres.NewReportRepo(postgres.New(pool)).CreateReport(context.Background(), report); err != nil {
		t.Fatalf("creating report %s: %v", report.ID, err)
	}
	return report
}

func TestReportRepo_CreateAndGetByReporterAndPost(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewReportRepo(postgres.New(pool))

	author := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rep-author"), domain.RoleMember, 50)
	reporter := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rep-reporter"), domain.RoleMember, 50)
	newPost(t, pool, "rep-post-1", author.ID, domain.PostVisible, time.Now())

	created := newReport(t, pool, reporter.ID, "rep-post-1", "pending", time.Now())

	got, err := repo.GetReportByReporterAndPost(ctx, reporter.ID, "rep-post-1")
	if err != nil {
		t.Fatalf("GetReportByReporterAndPost: %v", err)
	}
	if got.ID != created.ID || got.Reason != "spam" || got.Status != "pending" {
		t.Errorf("got %+v, want %+v", got, created)
	}
	if got.CreatedAt.IsZero() {
		t.Error("created_at came back zero")
	}
}

// "Has this person already reported this post?" is the duplicate check the
// report service runs first, so a miss must be ErrNotFound, not an error.
func TestReportRepo_GetReportByReporterAndPost_NotFound(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewReportRepo(postgres.New(pool))

	author := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rep-author"), domain.RoleMember, 50)
	reporter := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rep-reporter"), domain.RoleMember, 50)
	other := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rep-other"), domain.RoleMember, 50)
	newPost(t, pool, "rep-post-1", author.ID, domain.PostVisible, time.Now())
	newPost(t, pool, "rep-post-2", author.ID, domain.PostVisible, time.Now())
	newReport(t, pool, reporter.ID, "rep-post-1", "pending", time.Now())

	tests := []struct {
		name               string
		reporterID, postID string
	}{
		{"same reporter, different post", reporter.ID, "rep-post-2"},
		{"different reporter, same post", other.ID, "rep-post-1"},
		{"neither", other.ID, "rep-post-2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repo.GetReportByReporterAndPost(ctx, tt.reporterID, tt.postID)
			if !errors.Is(err, service.ErrNotFound) {
				t.Errorf("err = %v, want service.ErrNotFound", err)
			}
			if got != nil {
				t.Errorf("got %+v, want nil", got)
			}
		})
	}
}

// This count is the reporting rate limit, so the window boundary and the
// per-reporter scope both matter.
func TestReportRepo_CountReportsByReporterSince(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewReportRepo(postgres.New(pool))

	author := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rep-author"), domain.RoleMember, 50)
	reporter := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rep-reporter"), domain.RoleMember, 50)
	other := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rep-other"), domain.RoleMember, 50)

	now := time.Now()
	for i := 1; i <= 4; i++ {
		newPost(t, pool, fmt.Sprintf("rep-post-%d", i), author.ID, domain.PostVisible, now)
	}

	newReport(t, pool, reporter.ID, "rep-post-1", "pending", now.Add(-1*time.Hour))
	newReport(t, pool, reporter.ID, "rep-post-2", "pending", now.Add(-2*time.Hour))
	// Outside the window.
	newReport(t, pool, reporter.ID, "rep-post-3", "pending", now.Add(-48*time.Hour))
	// Another reporter's report must not count against this one's limit.
	newReport(t, pool, other.ID, "rep-post-4", "pending", now.Add(-1*time.Hour))

	got, err := repo.CountReportsByReporterSince(ctx, reporter.ID, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("CountReportsByReporterSince: %v", err)
	}
	if got != 2 {
		t.Errorf("count = %d, want 2 within the window", got)
	}

	if got, err := repo.CountReportsByReporterSince(ctx, "no-such-user", now.Add(-24*time.Hour)); err != nil || got != 0 {
		t.Errorf("count for an unknown reporter = %d, %v; want 0, nil", got, err)
	}
}

// The moderation queue: pending reports only, oldest first, with paging.
func TestReportRepo_ListPendingReports(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewReportRepo(postgres.New(pool))

	author := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rep-author"), domain.RoleMember, 50)
	reporter := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rep-reporter"), domain.RoleMember, 50)

	now := time.Now()
	for i := 1; i <= 4; i++ {
		newPost(t, pool, fmt.Sprintf("rep-post-%d", i), author.ID, domain.PostVisible, now)
	}

	oldest := newReport(t, pool, reporter.ID, "rep-post-1", "pending", now.Add(-3*time.Hour))
	middle := newReport(t, pool, reporter.ID, "rep-post-2", "pending", now.Add(-2*time.Hour))
	newest := newReport(t, pool, reporter.ID, "rep-post-3", "pending", now.Add(-1*time.Hour))
	// Already handled, so it is out of the queue.
	resolved := newReport(t, pool, reporter.ID, "rep-post-4", "resolved", now.Add(-4*time.Hour))

	got, err := repo.ListPendingReports(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListPendingReports: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d reports, want the 3 pending ones", len(got))
	}
	// Oldest first: the queue is worked front to back.
	if got[0].ID != oldest.ID || got[1].ID != middle.ID || got[2].ID != newest.ID {
		t.Errorf("order = %s, %s, %s; want oldest first", got[0].ID, got[1].ID, got[2].ID)
	}
	for _, r := range got {
		if r.ID == resolved.ID {
			t.Error("a resolved report is still in the queue")
		}
		if r.Status != "pending" {
			t.Errorf("report %s has status %q", r.ID, r.Status)
		}
	}

	page, err := repo.ListPendingReports(ctx, 2, 1)
	if err != nil {
		t.Fatalf("ListPendingReports (paged): %v", err)
	}
	if len(page) != 2 || page[0].ID != middle.ID || page[1].ID != newest.ID {
		t.Errorf("second page = %v, want [%s %s]", page, middle.ID, newest.ID)
	}

	past, err := repo.ListPendingReports(ctx, 10, 99)
	if err != nil {
		t.Fatalf("ListPendingReports (past the end): %v", err)
	}
	if len(past) != 0 {
		t.Errorf("got %d reports past the end, want 0", len(past))
	}
}

func TestReportRepo_UpdateReportStatus(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewReportRepo(postgres.New(pool))

	author := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rep-author"), domain.RoleMember, 50)
	reporter := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rep-reporter"), domain.RoleMember, 50)
	newPost(t, pool, "rep-post-1", author.ID, domain.PostVisible, time.Now())
	report := newReport(t, pool, reporter.ID, "rep-post-1", "pending", time.Now())

	updated, err := repo.UpdateReportStatus(ctx, report.ID, "resolved")
	if err != nil {
		t.Fatalf("UpdateReportStatus: %v", err)
	}
	if updated.Status != "resolved" {
		t.Errorf("status = %q, want %q", updated.Status, "resolved")
	}
	// The rest of the report is returned intact, not blanked by the UPDATE.
	if updated.ID != report.ID || updated.ReporterID != reporter.ID || updated.PostID != "rep-post-1" || updated.Reason != "spam" {
		t.Errorf("got %+v, want the other fields preserved from %+v", updated, report)
	}

	// And it has left the queue.
	pending, err := repo.ListPendingReports(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListPendingReports: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("queue still holds %d reports", len(pending))
	}
}

// Two moderators acting on the same report is a real race; the second one must
// get ErrNotFound rather than a raw pgx error.
func TestReportRepo_UpdateReportStatus_UnknownID(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewReportRepo(postgres.New(pool))

	got, err := repo.UpdateReportStatus(ctx, "no-such-report", "resolved")
	if !errors.Is(err, service.ErrNotFound) {
		t.Errorf("err = %v, want service.ErrNotFound", err)
	}
	if got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

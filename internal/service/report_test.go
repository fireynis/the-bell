package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// mockReportRepo is an in-memory ReportRepository for testing. It keeps reports
// in a single slice and derives every lookup from it, so the service's window
// and duplicate arguments are actually honoured rather than ignored.
type mockReportRepo struct {
	reports []*domain.Report

	createErr error
	lookupErr error
	countErr  error
	listErr   error
	updateErr error
}

func newMockReportRepo() *mockReportRepo {
	return &mockReportRepo{}
}

func (m *mockReportRepo) CreateReport(_ context.Context, report *domain.Report) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.reports = append(m.reports, report)
	return nil
}

func (m *mockReportRepo) GetReportByReporterAndPost(_ context.Context, reporterID, postID string) (*domain.Report, error) {
	if m.lookupErr != nil {
		return nil, m.lookupErr
	}
	for _, r := range m.reports {
		if r.ReporterID == reporterID && r.PostID == postID {
			return r, nil
		}
	}
	return nil, ErrNotFound
}

// CountReportsByReporterSince mirrors the SQL: created_at >= since.
func (m *mockReportRepo) CountReportsByReporterSince(_ context.Context, reporterID string, since time.Time) (int64, error) {
	if m.countErr != nil {
		return 0, m.countErr
	}
	var count int64
	for _, r := range m.reports {
		if r.ReporterID == reporterID && !r.CreatedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

func (m *mockReportRepo) ListPendingReports(_ context.Context, limit, offset int) ([]*domain.Report, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var pending []*domain.Report
	for _, r := range m.reports {
		if r.Status == "pending" {
			pending = append(pending, r)
		}
	}
	sortReportsByCreatedAt(pending)
	if offset >= len(pending) {
		return nil, nil
	}
	pending = pending[offset:]
	if len(pending) > limit {
		pending = pending[:limit]
	}
	return pending, nil
}

func (m *mockReportRepo) UpdateReportStatus(_ context.Context, id, status string) (*domain.Report, error) {
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	for _, r := range m.reports {
		if r.ID == id {
			r.Status = status
			return r, nil
		}
	}
	return nil, ErrNotFound
}

// sortReportsByCreatedAt puts the queue in the FIFO order the SQL guarantees.
func sortReportsByCreatedAt(reports []*domain.Report) {
	for i := 1; i < len(reports); i++ {
		for j := i; j > 0 && reports[j].CreatedAt.Before(reports[j-1].CreatedAt); j-- {
			reports[j], reports[j-1] = reports[j-1], reports[j]
		}
	}
}

// seedReport records a report that already exists, so the duplicate check and
// the hourly window have something to find.
func (m *mockReportRepo) seedReport(id, reporterID, postID, status string, createdAt time.Time) {
	m.reports = append(m.reports, &domain.Report{
		ID:         id,
		ReporterID: reporterID,
		PostID:     postID,
		Reason:     "seeded",
		Status:     status,
		CreatedAt:  createdAt,
	})
}

// mockReportPostGetter is an in-memory PostGetter for the report tests.
type mockReportPostGetter struct {
	posts  map[string]*domain.Post
	getErr error
}

func newMockReportPostGetter() *mockReportPostGetter {
	return &mockReportPostGetter{posts: make(map[string]*domain.Post)}
}

func (m *mockReportPostGetter) GetPostByID(_ context.Context, id string) (*domain.Post, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	p, ok := m.posts[id]
	if !ok {
		return nil, ErrNotFound
	}
	return p, nil
}

var reportNow = time.Date(2026, 3, 1, 14, 0, 0, 0, time.UTC)

func reportClock() time.Time { return reportNow }

// newReportTestHarness wires a service over a visible post authored by
// "author-1", which is what most of the report tests need.
func newReportTestHarness() (*mockReportRepo, *mockReportPostGetter, *ReportService) {
	repo := newMockReportRepo()
	posts := newMockReportPostGetter()
	posts.posts["post-1"] = &domain.Post{
		ID: "post-1", AuthorID: "author-1", Status: domain.PostVisible,
	}
	return repo, posts, NewReportService(repo, posts, reportClock)
}

// --- SubmitReport success path ---

func TestReportService_SubmitReport_Success(t *testing.T) {
	repo, _, svc := newReportTestHarness()

	report, err := svc.SubmitReport(context.Background(), "reporter-1", "post-1", "  this is spam  ")
	if err != nil {
		t.Fatalf("SubmitReport() unexpected error: %v", err)
	}
	if report.ID == "" {
		t.Error("SubmitReport() returned an empty ID")
	}
	if report.ReporterID != "reporter-1" {
		t.Errorf("ReporterID = %q, want %q", report.ReporterID, "reporter-1")
	}
	if report.PostID != "post-1" {
		t.Errorf("PostID = %q, want %q", report.PostID, "post-1")
	}
	// The stored reason is the trimmed one, not what the reporter typed.
	if report.Reason != "this is spam" {
		t.Errorf("Reason = %q, want %q", report.Reason, "this is spam")
	}
	if report.Status != "pending" {
		t.Errorf("Status = %q, want %q", report.Status, "pending")
	}
	if !report.CreatedAt.Equal(reportNow) {
		t.Errorf("CreatedAt = %v, want %v", report.CreatedAt, reportNow)
	}
	if len(repo.reports) != 1 {
		t.Fatalf("stored %d reports, want 1", len(repo.reports))
	}
	if repo.reports[0].ID != report.ID {
		t.Error("the returned report is not the one that was stored")
	}
}

// A nil clock must fall back to time.Now rather than producing a zero timestamp.
func TestReportService_SubmitReport_NilClockUsesWallTime(t *testing.T) {
	repo := newMockReportRepo()
	posts := newMockReportPostGetter()
	posts.posts["post-1"] = &domain.Post{ID: "post-1", AuthorID: "author-1", Status: domain.PostVisible}

	svc := NewReportService(repo, posts, nil)
	before := time.Now()
	report, err := svc.SubmitReport(context.Background(), "reporter-1", "post-1", "spam")
	if err != nil {
		t.Fatalf("SubmitReport() unexpected error: %v", err)
	}
	if report.CreatedAt.Before(before) || report.CreatedAt.After(time.Now()) {
		t.Errorf("CreatedAt = %v, want a timestamp between %v and now", report.CreatedAt, before)
	}
}

// --- SubmitReport validation ---

func TestReportService_SubmitReport_RejectsBadReason(t *testing.T) {
	tests := []struct {
		name   string
		reason string
	}{
		{"empty reason", ""},
		{"whitespace-only reason", "   \t  "},
		{"reason over the length limit", strings.Repeat("a", maxReportReasonLen+1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, _, svc := newReportTestHarness()
			_, err := svc.SubmitReport(context.Background(), "reporter-1", "post-1", tt.reason)
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("SubmitReport() error = %v, want %v", err, ErrValidation)
			}
			if len(repo.reports) != 0 {
				t.Error("a rejected report was stored anyway")
			}
		})
	}
}

func TestReportService_SubmitReport_PostNotFound(t *testing.T) {
	_, _, svc := newReportTestHarness()

	_, err := svc.SubmitReport(context.Background(), "reporter-1", "nonexistent", "spam")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("SubmitReport() error = %v, want %v", err, ErrNotFound)
	}
}

// An already-removed post cannot be reported: there is nothing left for a
// moderator to act on.
func TestReportService_SubmitReport_PostNotVisible(t *testing.T) {
	for _, status := range []domain.PostStatus{domain.PostRemovedByAuthor, domain.PostRemovedByMod} {
		t.Run(string(status), func(t *testing.T) {
			repo, posts, svc := newReportTestHarness()
			posts.posts["post-1"].Status = status

			_, err := svc.SubmitReport(context.Background(), "reporter-1", "post-1", "spam")
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("SubmitReport() error = %v, want %v", err, ErrValidation)
			}
			if len(repo.reports) != 0 {
				t.Error("a rejected report was stored anyway")
			}
		})
	}
}

func TestReportService_SubmitReport_SelfReport(t *testing.T) {
	repo, _, svc := newReportTestHarness()

	_, err := svc.SubmitReport(context.Background(), "author-1", "post-1", "spam")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("SubmitReport() error = %v, want %v", err, ErrValidation)
	}
	if !strings.Contains(err.Error(), "your own post") {
		t.Errorf("error = %q, want it to name the self-report rule", err)
	}
	if len(repo.reports) != 0 {
		t.Error("a self-report was stored anyway")
	}
}

func TestReportService_SubmitReport_Duplicate(t *testing.T) {
	repo, _, svc := newReportTestHarness()
	repo.seedReport("report-1", "reporter-1", "post-1", "pending", reportNow.Add(-24*time.Hour))

	_, err := svc.SubmitReport(context.Background(), "reporter-1", "post-1", "spam again")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("SubmitReport() error = %v, want %v", err, ErrValidation)
	}
	if !strings.Contains(err.Error(), "already reported") {
		t.Errorf("error = %q, want it to name the duplicate rule", err)
	}
	if len(repo.reports) != 1 {
		t.Errorf("stored %d reports, want the seeded 1", len(repo.reports))
	}
}

// A report on a different post by the same reporter is not a duplicate.
func TestReportService_SubmitReport_DuplicateIsPerPost(t *testing.T) {
	repo, posts, svc := newReportTestHarness()
	posts.posts["post-2"] = &domain.Post{ID: "post-2", AuthorID: "author-1", Status: domain.PostVisible}
	repo.seedReport("report-1", "reporter-1", "post-1", "pending", reportNow.Add(-24*time.Hour))

	if _, err := svc.SubmitReport(context.Background(), "reporter-1", "post-2", "spam"); err != nil {
		t.Fatalf("SubmitReport() unexpected error: %v", err)
	}
}

// --- SubmitReport hourly rate limit ---

func TestReportService_SubmitReport_HourlyLimit(t *testing.T) {
	tests := []struct {
		name      string
		age       time.Duration // how long before now each seeded report was filed
		count     int
		wantLimit bool
	}{
		{"under the limit within the hour", 30 * time.Minute, hourlyReportLimit - 1, false},
		{"at the limit within the hour", 30 * time.Minute, hourlyReportLimit, true},
		{"over the limit within the hour", 30 * time.Minute, hourlyReportLimit + 3, true},
		{"the same volume an hour earlier does not count", 2 * time.Hour, hourlyReportLimit + 3, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, posts, svc := newReportTestHarness()
			// Each seeded report targets its own post so the duplicate check
			// cannot fire before the rate limit does.
			for i := 0; i < tt.count; i++ {
				postID := "seeded-post-" + string(rune('a'+i))
				posts.posts[postID] = &domain.Post{ID: postID, AuthorID: "author-1", Status: domain.PostVisible}
				repo.seedReport("seeded-"+postID, "reporter-1", postID, "pending", reportNow.Add(-tt.age))
			}

			_, err := svc.SubmitReport(context.Background(), "reporter-1", "post-1", "spam")
			if tt.wantLimit {
				if !errors.Is(err, ErrRateLimit) {
					t.Fatalf("SubmitReport() error = %v, want %v", err, ErrRateLimit)
				}
				return
			}
			if err != nil {
				t.Fatalf("SubmitReport() unexpected error: %v", err)
			}
		})
	}
}

// The limit is per reporter: one prolific reporter must not lock everyone else
// out.
func TestReportService_SubmitReport_LimitIsPerReporter(t *testing.T) {
	repo, posts, svc := newReportTestHarness()
	for i := 0; i < hourlyReportLimit; i++ {
		postID := "noisy-post-" + string(rune('a'+i))
		posts.posts[postID] = &domain.Post{ID: postID, AuthorID: "author-1", Status: domain.PostVisible}
		repo.seedReport("noisy-"+postID, "noisy-reporter", postID, "pending", reportNow.Add(-10*time.Minute))
	}

	if _, err := svc.SubmitReport(context.Background(), "quiet-reporter", "post-1", "spam"); err != nil {
		t.Fatalf("SubmitReport() unexpected error for an unrelated reporter: %v", err)
	}
}

// --- SubmitReport branch ordering ---

// The checks run in a fixed order, and the first failure wins. These pin that
// order: a reporter who breaks several rules at once always sees the earliest
// one, which is what the API's error messages depend on.
func TestReportService_SubmitReport_CheckOrdering(t *testing.T) {
	overLimit := func(repo *mockReportRepo, posts *mockReportPostGetter, reporterID string) {
		for i := 0; i < hourlyReportLimit; i++ {
			postID := "limit-post-" + string(rune('a'+i))
			posts.posts[postID] = &domain.Post{ID: postID, AuthorID: "author-1", Status: domain.PostVisible}
			repo.seedReport("limit-"+postID, reporterID, postID, "pending", reportNow.Add(-10*time.Minute))
		}
	}

	t.Run("bad reason beats a missing post", func(t *testing.T) {
		_, _, svc := newReportTestHarness()
		_, err := svc.SubmitReport(context.Background(), "reporter-1", "nonexistent", "")
		if !errors.Is(err, ErrValidation) || errors.Is(err, ErrNotFound) {
			t.Fatalf("error = %v, want the reason validation error", err)
		}
	})

	t.Run("invisible post beats a self-report", func(t *testing.T) {
		_, posts, svc := newReportTestHarness()
		posts.posts["post-1"].Status = domain.PostRemovedByMod

		_, err := svc.SubmitReport(context.Background(), "author-1", "post-1", "spam")
		if !strings.Contains(err.Error(), "not visible") {
			t.Fatalf("error = %v, want the visibility error", err)
		}
	})

	t.Run("self-report beats a duplicate", func(t *testing.T) {
		repo, _, svc := newReportTestHarness()
		repo.seedReport("report-1", "author-1", "post-1", "pending", reportNow.Add(-time.Minute))

		_, err := svc.SubmitReport(context.Background(), "author-1", "post-1", "spam")
		if !strings.Contains(err.Error(), "your own post") {
			t.Fatalf("error = %v, want the self-report error", err)
		}
	})

	t.Run("self-report beats the rate limit", func(t *testing.T) {
		repo, posts, svc := newReportTestHarness()
		overLimit(repo, posts, "author-1")

		_, err := svc.SubmitReport(context.Background(), "author-1", "post-1", "spam")
		if errors.Is(err, ErrRateLimit) {
			t.Fatalf("error = %v, want the self-report error, not the rate limit", err)
		}
		if !strings.Contains(err.Error(), "your own post") {
			t.Fatalf("error = %v, want the self-report error", err)
		}
	})

	t.Run("duplicate beats the rate limit", func(t *testing.T) {
		repo, posts, svc := newReportTestHarness()
		overLimit(repo, posts, "reporter-1")
		repo.seedReport("dup", "reporter-1", "post-1", "pending", reportNow.Add(-time.Minute))

		_, err := svc.SubmitReport(context.Background(), "reporter-1", "post-1", "spam")
		if errors.Is(err, ErrRateLimit) {
			t.Fatalf("error = %v, want the duplicate error, not the rate limit", err)
		}
		if !strings.Contains(err.Error(), "already reported") {
			t.Fatalf("error = %v, want the duplicate error", err)
		}
	})
}

// --- SubmitReport error propagation ---

func TestReportService_SubmitReport_RepoErrors(t *testing.T) {
	dbDown := errors.New("db connection lost")

	tests := []struct {
		name  string
		setup func(*mockReportRepo, *mockReportPostGetter)
	}{
		{"post lookup fails", func(_ *mockReportRepo, p *mockReportPostGetter) { p.getErr = dbDown }},
		{"duplicate lookup fails", func(r *mockReportRepo, _ *mockReportPostGetter) { r.lookupErr = dbDown }},
		{"rate-limit count fails", func(r *mockReportRepo, _ *mockReportPostGetter) { r.countErr = dbDown }},
		{"insert fails", func(r *mockReportRepo, _ *mockReportPostGetter) { r.createErr = dbDown }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, posts, svc := newReportTestHarness()
			tt.setup(repo, posts)

			report, err := svc.SubmitReport(context.Background(), "reporter-1", "post-1", "spam")
			if err == nil {
				t.Fatal("SubmitReport() expected an error, got nil")
			}
			if report != nil {
				t.Errorf("SubmitReport() = %v, want nil alongside the error", report)
			}
			if !errors.Is(err, dbDown) {
				t.Errorf("error = %v, want it to wrap %v", err, dbDown)
			}
		})
	}
}

// A missing prior report is the normal case, not a failure — ErrNotFound from
// the duplicate lookup must not abort the submission.
func TestReportService_SubmitReport_NotFoundFromDuplicateLookupIsNotAnError(t *testing.T) {
	repo, _, svc := newReportTestHarness()
	repo.lookupErr = ErrNotFound

	if _, err := svc.SubmitReport(context.Background(), "reporter-1", "post-1", "spam"); err != nil {
		t.Fatalf("SubmitReport() unexpected error: %v", err)
	}
}

// --- ListQueue ---

func TestReportService_ListQueue(t *testing.T) {
	repo, _, svc := newReportTestHarness()
	repo.seedReport("oldest", "reporter-1", "post-a", "pending", reportNow.Add(-3*time.Hour))
	repo.seedReport("newest", "reporter-2", "post-b", "pending", reportNow.Add(-time.Hour))
	repo.seedReport("handled", "reporter-3", "post-c", "reviewed", reportNow.Add(-2*time.Hour))

	queue, err := svc.ListQueue(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("ListQueue() unexpected error: %v", err)
	}
	if len(queue) != 2 {
		t.Fatalf("ListQueue() returned %d reports, want 2 pending", len(queue))
	}
	if queue[0].ID != "oldest" || queue[1].ID != "newest" {
		t.Errorf("ListQueue() order = [%s %s], want FIFO [oldest newest]", queue[0].ID, queue[1].ID)
	}
}

func TestReportService_ListQueue_Paginates(t *testing.T) {
	repo, _, svc := newReportTestHarness()
	repo.seedReport("first", "reporter-1", "post-a", "pending", reportNow.Add(-3*time.Hour))
	repo.seedReport("second", "reporter-2", "post-b", "pending", reportNow.Add(-2*time.Hour))

	page, err := svc.ListQueue(context.Background(), 1, 1)
	if err != nil {
		t.Fatalf("ListQueue() unexpected error: %v", err)
	}
	if len(page) != 1 || page[0].ID != "second" {
		t.Errorf("ListQueue(limit=1, offset=1) = %v, want just the second report", page)
	}
}

func TestReportService_ListQueue_RepoError(t *testing.T) {
	repo, _, svc := newReportTestHarness()
	repo.listErr = errors.New("db connection lost")

	if _, err := svc.ListQueue(context.Background(), 10, 0); err == nil {
		t.Fatal("ListQueue() expected an error, got nil")
	}
}

// --- UpdateStatus ---

func TestReportService_UpdateStatus(t *testing.T) {
	tests := []struct {
		name    string
		status  string
		wantErr bool
	}{
		{"reviewed is a terminal state", "reviewed", false},
		{"dismissed is a terminal state", "dismissed", false},
		{"pending cannot be re-entered", "pending", true},
		{"empty status is rejected", "", true},
		{"arbitrary status is rejected", "resolved", true},
		{"casing must match exactly", "Reviewed", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, _, svc := newReportTestHarness()
			repo.seedReport("report-1", "reporter-1", "post-1", "pending", reportNow)

			report, err := svc.UpdateStatus(context.Background(), "report-1", tt.status)
			if tt.wantErr {
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("UpdateStatus(%q) error = %v, want %v", tt.status, err, ErrValidation)
				}
				if repo.reports[0].Status != "pending" {
					t.Errorf("stored status = %q, want it untouched at %q", repo.reports[0].Status, "pending")
				}
				return
			}
			if err != nil {
				t.Fatalf("UpdateStatus(%q) unexpected error: %v", tt.status, err)
			}
			if report.Status != tt.status {
				t.Errorf("returned status = %q, want %q", report.Status, tt.status)
			}
			if repo.reports[0].Status != tt.status {
				t.Errorf("stored status = %q, want %q", repo.reports[0].Status, tt.status)
			}
		})
	}
}

func TestReportService_UpdateStatus_NotFound(t *testing.T) {
	_, _, svc := newReportTestHarness()

	if _, err := svc.UpdateStatus(context.Background(), "nonexistent", "reviewed"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateStatus() error = %v, want %v", err, ErrNotFound)
	}
}

// The queue read joins the reporter by name, and the service passes the report
// through untouched. A service that rebuilt domain.Report field by field would
// drop the name and send the moderation queue back to raw ids.
func TestReportService_ListQueue_CarriesTheReporterDisplayName(t *testing.T) {
	repo, _, svc := newReportTestHarness()
	repo.seedReport("r1", "reporter-1", "post-a", "pending", reportNow.Add(-time.Hour))
	repo.reports[0].ReporterDisplayName = "Alice"

	queue, err := svc.ListQueue(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("ListQueue() unexpected error: %v", err)
	}
	if len(queue) != 1 {
		t.Fatalf("%d reports, want 1", len(queue))
	}
	if queue[0].ReporterDisplayName != "Alice" {
		t.Errorf("reporter_display_name = %q, want Alice", queue[0].ReporterDisplayName)
	}
}

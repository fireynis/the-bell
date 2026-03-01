package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/handler"
	"github.com/fireynis/the-bell/internal/service"
)

// --- mock ReportRepository ---

type mockReportRepo struct {
	reports map[string]*domain.Report
}

func newMockReportRepo() *mockReportRepo {
	return &mockReportRepo{reports: make(map[string]*domain.Report)}
}

func (m *mockReportRepo) CreateReport(_ context.Context, report *domain.Report) error {
	m.reports[report.ID] = report
	return nil
}

func (m *mockReportRepo) GetReportByReporterAndPost(_ context.Context, reporterID, postID string) (*domain.Report, error) {
	for _, r := range m.reports {
		if r.ReporterID == reporterID && r.PostID == postID {
			return r, nil
		}
	}
	return nil, service.ErrNotFound
}

func (m *mockReportRepo) CountReportsByReporterSince(_ context.Context, reporterID string, since time.Time) (int64, error) {
	var count int64
	for _, r := range m.reports {
		if r.ReporterID == reporterID && !r.CreatedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

func (m *mockReportRepo) ListPendingReports(_ context.Context, limit, offset int) ([]*domain.Report, error) {
	var result []*domain.Report
	for _, r := range m.reports {
		if r.Status == "pending" {
			result = append(result, r)
		}
	}
	if offset > len(result) {
		return []*domain.Report{}, nil
	}
	result = result[offset:]
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *mockReportRepo) UpdateReportStatus(_ context.Context, id, status string) (*domain.Report, error) {
	r, ok := m.reports[id]
	if !ok {
		return nil, service.ErrNotFound
	}
	r.Status = status
	return r, nil
}

// --- mock PostGetter ---

type mockPostGetter struct {
	posts map[string]*domain.Post
}

func newMockPostGetter() *mockPostGetter {
	return &mockPostGetter{posts: make(map[string]*domain.Post)}
}

func (m *mockPostGetter) GetPostByID(_ context.Context, id string) (*domain.Post, error) {
	p, ok := m.posts[id]
	if !ok {
		return nil, service.ErrNotFound
	}
	return p, nil
}

// --- test helpers ---

func newTestReportService(reports service.ReportRepository, posts service.PostGetter) *service.ReportService {
	return service.NewReportService(reports, posts, func() time.Time { return fixedNow })
}

func testModerator() *domain.User {
	return &domain.User{
		ID:       "mod-1",
		Role:     domain.RoleModerator,
		IsActive: true,
	}
}

// --- SubmitReport tests ---

func TestReportHandler_SubmitReport(t *testing.T) {
	reportRepo := newMockReportRepo()
	postGetter := newMockPostGetter()
	postGetter.posts["post-1"] = &domain.Post{
		ID:       "post-1",
		AuthorID: "user-other",
		Status:   domain.PostVisible,
	}
	svc := newTestReportService(reportRepo, postGetter)
	h := handler.NewReportHandler(svc)

	body := `{"reason":"This post is offensive"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts/post-1/report", strings.NewReader(body))
	req = withChiURLParam(req, "id", "post-1")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.SubmitReport(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var report domain.Report
	decodeBody(t, rec, &report)
	if report.ID == "" {
		t.Error("expected non-empty report ID")
	}
	if report.PostID != "post-1" {
		t.Errorf("post_id = %q, want %q", report.PostID, "post-1")
	}
	if report.ReporterID != "user-1" {
		t.Errorf("reporter_id = %q, want %q", report.ReporterID, "user-1")
	}
	if report.Status != "pending" {
		t.Errorf("status = %q, want %q", report.Status, "pending")
	}
}

func TestReportHandler_SubmitReport_NoUser(t *testing.T) {
	reportRepo := newMockReportRepo()
	postGetter := newMockPostGetter()
	svc := newTestReportService(reportRepo, postGetter)
	h := handler.NewReportHandler(svc)

	body := `{"reason":"spam"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts/post-1/report", strings.NewReader(body))
	req = withChiURLParam(req, "id", "post-1")
	rec := httptest.NewRecorder()

	h.SubmitReport(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestReportHandler_SubmitReport_PostNotFound(t *testing.T) {
	reportRepo := newMockReportRepo()
	postGetter := newMockPostGetter()
	svc := newTestReportService(reportRepo, postGetter)
	h := handler.NewReportHandler(svc)

	body := `{"reason":"spam"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts/nonexistent/report", strings.NewReader(body))
	req = withChiURLParam(req, "id", "nonexistent")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.SubmitReport(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestReportHandler_SubmitReport_EmptyReason(t *testing.T) {
	reportRepo := newMockReportRepo()
	postGetter := newMockPostGetter()
	postGetter.posts["post-1"] = &domain.Post{
		ID:       "post-1",
		AuthorID: "user-other",
		Status:   domain.PostVisible,
	}
	svc := newTestReportService(reportRepo, postGetter)
	h := handler.NewReportHandler(svc)

	body := `{"reason":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts/post-1/report", strings.NewReader(body))
	req = withChiURLParam(req, "id", "post-1")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.SubmitReport(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestReportHandler_SubmitReport_InvalidJSON(t *testing.T) {
	reportRepo := newMockReportRepo()
	postGetter := newMockPostGetter()
	svc := newTestReportService(reportRepo, postGetter)
	h := handler.NewReportHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts/post-1/report", strings.NewReader(`{invalid`))
	req = withChiURLParam(req, "id", "post-1")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.SubmitReport(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestReportHandler_SubmitReport_SelfReport(t *testing.T) {
	reportRepo := newMockReportRepo()
	postGetter := newMockPostGetter()
	postGetter.posts["post-1"] = &domain.Post{
		ID:       "post-1",
		AuthorID: "user-1", // same as test user
		Status:   domain.PostVisible,
	}
	svc := newTestReportService(reportRepo, postGetter)
	h := handler.NewReportHandler(svc)

	body := `{"reason":"reporting myself"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts/post-1/report", strings.NewReader(body))
	req = withChiURLParam(req, "id", "post-1")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.SubmitReport(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestReportHandler_SubmitReport_Duplicate(t *testing.T) {
	reportRepo := newMockReportRepo()
	reportRepo.reports["existing"] = &domain.Report{
		ID:         "existing",
		ReporterID: "user-1",
		PostID:     "post-1",
		Reason:     "already reported",
		Status:     "pending",
		CreatedAt:  fixedNow,
	}
	postGetter := newMockPostGetter()
	postGetter.posts["post-1"] = &domain.Post{
		ID:       "post-1",
		AuthorID: "user-other",
		Status:   domain.PostVisible,
	}
	svc := newTestReportService(reportRepo, postGetter)
	h := handler.NewReportHandler(svc)

	body := `{"reason":"reporting again"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts/post-1/report", strings.NewReader(body))
	req = withChiURLParam(req, "id", "post-1")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.SubmitReport(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestReportHandler_SubmitReport_RateLimit(t *testing.T) {
	reportRepo := newMockReportRepo()
	// Pre-fill with 5 reports within the last hour
	for i := 0; i < 5; i++ {
		reportRepo.reports[string(rune('a'+i))] = &domain.Report{
			ID:         string(rune('a' + i)),
			ReporterID: "user-1",
			PostID:     "other-post-" + string(rune('a'+i)),
			Reason:     "spam",
			Status:     "pending",
			CreatedAt:  fixedNow.Add(-30 * time.Minute), // within the hour
		}
	}
	postGetter := newMockPostGetter()
	postGetter.posts["post-new"] = &domain.Post{
		ID:       "post-new",
		AuthorID: "user-other",
		Status:   domain.PostVisible,
	}
	svc := newTestReportService(reportRepo, postGetter)
	h := handler.NewReportHandler(svc)

	body := `{"reason":"one too many"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts/post-new/report", strings.NewReader(body))
	req = withChiURLParam(req, "id", "post-new")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.SubmitReport(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}
}

func TestReportHandler_SubmitReport_RemovedPost(t *testing.T) {
	reportRepo := newMockReportRepo()
	postGetter := newMockPostGetter()
	postGetter.posts["post-1"] = &domain.Post{
		ID:       "post-1",
		AuthorID: "user-other",
		Status:   domain.PostRemovedByAuthor,
	}
	svc := newTestReportService(reportRepo, postGetter)
	h := handler.NewReportHandler(svc)

	body := `{"reason":"already removed"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts/post-1/report", strings.NewReader(body))
	req = withChiURLParam(req, "id", "post-1")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.SubmitReport(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// --- ListQueue tests ---

func TestReportHandler_ListQueue(t *testing.T) {
	reportRepo := newMockReportRepo()
	reportRepo.reports["r1"] = &domain.Report{
		ID:         "r1",
		ReporterID: "user-1",
		PostID:     "post-1",
		Reason:     "spam",
		Status:     "pending",
		CreatedAt:  fixedNow,
	}
	reportRepo.reports["r2"] = &domain.Report{
		ID:         "r2",
		ReporterID: "user-2",
		PostID:     "post-2",
		Reason:     "harassment",
		Status:     "pending",
		CreatedAt:  fixedNow,
	}
	postGetter := newMockPostGetter()
	svc := newTestReportService(reportRepo, postGetter)
	h := handler.NewReportHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/moderation/queue", nil)
	req = withUser(req, testModerator())
	rec := httptest.NewRecorder()

	h.ListQueue(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Reports []domain.Report `json:"reports"`
	}
	decodeBody(t, rec, &resp)

	if len(resp.Reports) != 2 {
		t.Errorf("got %d reports, want 2", len(resp.Reports))
	}
}

func TestReportHandler_ListQueue_Empty(t *testing.T) {
	reportRepo := newMockReportRepo()
	postGetter := newMockPostGetter()
	svc := newTestReportService(reportRepo, postGetter)
	h := handler.NewReportHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/moderation/queue", nil)
	req = withUser(req, testModerator())
	rec := httptest.NewRecorder()

	h.ListQueue(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Reports []domain.Report `json:"reports"`
	}
	decodeBody(t, rec, &resp)

	if resp.Reports == nil {
		t.Error("expected empty array, got null")
	}
	if len(resp.Reports) != 0 {
		t.Errorf("got %d reports, want 0", len(resp.Reports))
	}
}

// --- UpdateReportStatus tests ---

func TestReportHandler_UpdateReportStatus(t *testing.T) {
	reportRepo := newMockReportRepo()
	reportRepo.reports["r1"] = &domain.Report{
		ID:     "r1",
		Status: "pending",
	}
	postGetter := newMockPostGetter()
	svc := newTestReportService(reportRepo, postGetter)
	h := handler.NewReportHandler(svc)

	body := `{"status":"dismissed"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/moderation/reports/r1", strings.NewReader(body))
	req = withChiURLParam(req, "id", "r1")
	req = withUser(req, testModerator())
	rec := httptest.NewRecorder()

	h.UpdateReportStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var report domain.Report
	decodeBody(t, rec, &report)
	if report.Status != "dismissed" {
		t.Errorf("status = %q, want %q", report.Status, "dismissed")
	}
}

func TestReportHandler_UpdateReportStatus_Reviewed(t *testing.T) {
	reportRepo := newMockReportRepo()
	reportRepo.reports["r1"] = &domain.Report{
		ID:     "r1",
		Status: "pending",
	}
	postGetter := newMockPostGetter()
	svc := newTestReportService(reportRepo, postGetter)
	h := handler.NewReportHandler(svc)

	body := `{"status":"reviewed"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/moderation/reports/r1", strings.NewReader(body))
	req = withChiURLParam(req, "id", "r1")
	req = withUser(req, testModerator())
	rec := httptest.NewRecorder()

	h.UpdateReportStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestReportHandler_UpdateReportStatus_InvalidStatus(t *testing.T) {
	reportRepo := newMockReportRepo()
	reportRepo.reports["r1"] = &domain.Report{
		ID:     "r1",
		Status: "pending",
	}
	postGetter := newMockPostGetter()
	svc := newTestReportService(reportRepo, postGetter)
	h := handler.NewReportHandler(svc)

	body := `{"status":"approved"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/moderation/reports/r1", strings.NewReader(body))
	req = withChiURLParam(req, "id", "r1")
	req = withUser(req, testModerator())
	rec := httptest.NewRecorder()

	h.UpdateReportStatus(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestReportHandler_UpdateReportStatus_NotFound(t *testing.T) {
	reportRepo := newMockReportRepo()
	postGetter := newMockPostGetter()
	svc := newTestReportService(reportRepo, postGetter)
	h := handler.NewReportHandler(svc)

	body := `{"status":"dismissed"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/moderation/reports/nonexistent", strings.NewReader(body))
	req = withChiURLParam(req, "id", "nonexistent")
	req = withUser(req, testModerator())
	rec := httptest.NewRecorder()

	h.UpdateReportStatus(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestReportHandler_UpdateReportStatus_InvalidJSON(t *testing.T) {
	reportRepo := newMockReportRepo()
	postGetter := newMockPostGetter()
	svc := newTestReportService(reportRepo, postGetter)
	h := handler.NewReportHandler(svc)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/moderation/reports/r1", strings.NewReader(`{bad}`))
	req = withChiURLParam(req, "id", "r1")
	req = withUser(req, testModerator())
	rec := httptest.NewRecorder()

	h.UpdateReportStatus(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestReportHandler_ListQueue_FiltersDismissed(t *testing.T) {
	reportRepo := newMockReportRepo()
	reportRepo.reports["r1"] = &domain.Report{
		ID:     "r1",
		Status: "pending",
	}
	reportRepo.reports["r2"] = &domain.Report{
		ID:     "r2",
		Status: "dismissed",
	}
	postGetter := newMockPostGetter()
	svc := newTestReportService(reportRepo, postGetter)
	h := handler.NewReportHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/moderation/queue", nil)
	req = withUser(req, testModerator())
	rec := httptest.NewRecorder()

	h.ListQueue(rec, req)

	var resp struct {
		Reports []domain.Report `json:"reports"`
	}
	decodeBody(t, rec, &resp)

	if len(resp.Reports) != 1 {
		t.Errorf("got %d reports, want 1 (only pending)", len(resp.Reports))
	}
}

package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/google/uuid"
)

const (
	hourlyReportLimit   = 5
	maxReportReasonLen  = 1000
)

// ReportRepository abstracts report persistence using domain types.
type ReportRepository interface {
	CreateReport(ctx context.Context, report *domain.Report) error
	GetReportByReporterAndPost(ctx context.Context, reporterID, postID string) (*domain.Report, error)
	CountReportsByReporterSince(ctx context.Context, reporterID string, since time.Time) (int64, error)
	ListPendingReports(ctx context.Context, limit, offset int) ([]*domain.Report, error)
	UpdateReportStatus(ctx context.Context, id, status string) (*domain.Report, error)
}

// PostGetter retrieves a post by ID.
type PostGetter interface {
	GetPostByID(ctx context.Context, id string) (*domain.Post, error)
}

// ReportService orchestrates report business logic.
type ReportService struct {
	reports ReportRepository
	posts   PostGetter
	now     func() time.Time
}

func NewReportService(reports ReportRepository, posts PostGetter, clock func() time.Time) *ReportService {
	if clock == nil {
		clock = time.Now
	}
	return &ReportService{
		reports: reports,
		posts:   posts,
		now:     clock,
	}
}

// SubmitReport creates a new report for a post.
func (s *ReportService) SubmitReport(ctx context.Context, reporterID, postID, reason string) (*domain.Report, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: reason must not be empty", ErrValidation)
	}
	if len(reason) > maxReportReasonLen {
		return nil, fmt.Errorf("%w: reason exceeds %d characters", ErrValidation, maxReportReasonLen)
	}

	post, err := s.posts.GetPostByID(ctx, postID)
	if err != nil {
		return nil, err
	}

	if post.Status != domain.PostVisible {
		return nil, fmt.Errorf("%w: post is not visible", ErrValidation)
	}

	if post.AuthorID == reporterID {
		return nil, fmt.Errorf("%w: cannot report your own post", ErrValidation)
	}

	existing, err := s.reports.GetReportByReporterAndPost(ctx, reporterID, postID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("checking existing report: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("%w: you have already reported this post", ErrValidation)
	}

	now := s.now()
	since := now.Add(-1 * time.Hour)
	count, err := s.reports.CountReportsByReporterSince(ctx, reporterID, since)
	if err != nil {
		return nil, fmt.Errorf("counting recent reports: %w", err)
	}
	if count >= hourlyReportLimit {
		return nil, ErrRateLimit
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generating report id: %w", err)
	}

	report := &domain.Report{
		ID:         id.String(),
		ReporterID: reporterID,
		PostID:     postID,
		Reason:     reason,
		Status:     "pending",
		CreatedAt:  now,
	}

	if err := s.reports.CreateReport(ctx, report); err != nil {
		return nil, fmt.Errorf("creating report: %w", err)
	}

	return report, nil
}

// ListQueue returns pending reports in FIFO order.
func (s *ReportService) ListQueue(ctx context.Context, limit, offset int) ([]*domain.Report, error) {
	return s.reports.ListPendingReports(ctx, limit, offset)
}

// UpdateStatus updates a report's status to reviewed or dismissed.
func (s *ReportService) UpdateStatus(ctx context.Context, reportID, status string) (*domain.Report, error) {
	if status != "reviewed" && status != "dismissed" {
		return nil, fmt.Errorf("%w: status must be 'reviewed' or 'dismissed'", ErrValidation)
	}
	return s.reports.UpdateReportStatus(ctx, reportID, status)
}

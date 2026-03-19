package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ReportRepo adapts sqlc queries to the service.ReportRepository interface.
type ReportRepo struct {
	q *Queries
}

func NewReportRepo(q *Queries) *ReportRepo {
	return &ReportRepo{q: q}
}

func (r *ReportRepo) CreateReport(ctx context.Context, report *domain.Report) error {
	_, err := r.q.CreateReport(ctx, CreateReportParams{
		ID:         report.ID,
		ReporterID: report.ReporterID,
		PostID:     report.PostID,
		Reason:     report.Reason,
		Status:     report.Status,
		CreatedAt:  pgtype.Timestamptz{Time: report.CreatedAt, Valid: true},
	})
	return err
}

func (r *ReportRepo) GetReportByReporterAndPost(ctx context.Context, reporterID, postID string) (*domain.Report, error) {
	row, err := r.q.GetReportByReporterAndPost(ctx, GetReportByReporterAndPostParams{
		ReporterID: reporterID,
		PostID:     postID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, service.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return reportFromRow(row), nil
}

func (r *ReportRepo) CountReportsByReporterSince(ctx context.Context, reporterID string, since time.Time) (int64, error) {
	return r.q.CountReportsByReporterSince(ctx, CountReportsByReporterSinceParams{
		ReporterID: reporterID,
		CreatedAt:  pgtype.Timestamptz{Time: since, Valid: true},
	})
}

func (r *ReportRepo) ListPendingReports(ctx context.Context, limit, offset int) ([]*domain.Report, error) {
	rows, err := r.q.ListPendingReports(ctx, ListPendingReportsParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, err
	}

	reports := make([]*domain.Report, len(rows))
	for i, row := range rows {
		reports[i] = reportFromRow(row)
	}
	return reports, nil
}

func (r *ReportRepo) UpdateReportStatus(ctx context.Context, id, status string) (*domain.Report, error) {
	row, err := r.q.UpdateReportStatus(ctx, UpdateReportStatusParams{
		ID:     id,
		Status: status,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, service.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return reportFromRow(row), nil
}

func reportFromRow(row Report) *domain.Report {
	return &domain.Report{
		ID:         row.ID,
		ReporterID: row.ReporterID,
		PostID:     row.PostID,
		Reason:     row.Reason,
		Status:     row.Status,
		CreatedAt:  row.CreatedAt.Time,
	}
}

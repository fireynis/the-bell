-- name: CreateReport :one
INSERT INTO reports (id, reporter_id, post_id, reason, status, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetReportByID :one
SELECT * FROM reports WHERE id = $1;

-- name: GetReportByReporterAndPost :one
SELECT * FROM reports WHERE reporter_id = $1 AND post_id = $2;

-- name: CountReportsByReporterSince :one
SELECT COUNT(*) FROM reports
WHERE reporter_id = $1 AND created_at >= $2;

-- name: ListPendingReports :many
SELECT * FROM reports
WHERE status = 'pending'
ORDER BY created_at ASC
LIMIT $1 OFFSET $2;

-- name: UpdateReportStatus :one
UPDATE reports
SET status = $2
WHERE id = $1
RETURNING *;

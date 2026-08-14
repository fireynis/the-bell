-- name: CreateReport :one
INSERT INTO reports (id, reporter_id, post_id, reason, status, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetReportByReporterAndPost :one
SELECT * FROM reports WHERE reporter_id = $1 AND post_id = $2;

-- name: CountReportsByReporterSince :one
SELECT COUNT(*) FROM reports
WHERE reporter_id = $1 AND created_at >= $2;

-- name: ListPendingReports :many
-- The reporter is joined by name: this is the moderation queue, and a moderator
-- weighing a report has to know who filed it, which a raw id does not tell
-- them. reporter_id is a NOT NULL reference to users, so the inner join can
-- drop no pending report. Only the queue joins — the single-report reads serve
-- the reporter's own submission, where the name is already known.
SELECT sqlc.embed(r),
       u.display_name AS reporter_display_name
FROM reports r
JOIN users u ON u.id = r.reporter_id
WHERE r.status = 'pending'
ORDER BY r.created_at ASC
LIMIT $1 OFFSET $2;

-- name: UpdateReportStatus :one
UPDATE reports
SET status = $2
WHERE id = $1
RETURNING *;

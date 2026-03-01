-- name: CreateModerationAction :one
INSERT INTO moderation_actions (id, target_user_id, moderator_id, action_type, severity, reason, duration_seconds, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetModerationActionByID :one
SELECT * FROM moderation_actions WHERE id = $1;

-- name: ListModerationActionsByTarget :many
SELECT * FROM moderation_actions
WHERE target_user_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

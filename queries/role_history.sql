-- name: CreateRoleHistoryEntry :one
INSERT INTO role_history (id, user_id, old_role, new_role, reason, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

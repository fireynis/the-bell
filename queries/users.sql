-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByKratosID :one
SELECT * FROM users WHERE kratos_identity_id = $1;

-- name: CreateUser :one
INSERT INTO users (id, kratos_identity_id, display_name, bio, avatar_url, trust_score, role, is_active, joined_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: UpdateUserProfile :one
UPDATE users
SET display_name = $2, bio = $3, avatar_url = $4, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateUserTrustScore :exec
UPDATE users SET trust_score = $2, updated_at = NOW() WHERE id = $1;

-- name: UpdateUserRole :exec
UPDATE users SET role = $2, updated_at = NOW() WHERE id = $1;

-- name: ListUsersByRole :many
SELECT * FROM users WHERE role = $1 ORDER BY created_at DESC LIMIT $2;

-- name: ListPendingUsers :many
SELECT * FROM users
WHERE role = 'pending' AND is_active = TRUE
ORDER BY created_at ASC;

-- name: CountUsersByMinRole :one
SELECT COUNT(*) FROM users
WHERE role IN ('member', 'moderator', 'council') AND is_active = TRUE;

-- name: DeactivateUser :exec
UPDATE users SET is_active = FALSE, updated_at = NOW() WHERE id = $1;

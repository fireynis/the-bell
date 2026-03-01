-- name: AddReaction :one
INSERT INTO reactions (id, user_id, post_id, reaction_type, created_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, post_id, reaction_type) DO NOTHING
RETURNING *;

-- name: RemoveReaction :exec
DELETE FROM reactions WHERE user_id = $1 AND post_id = $2 AND reaction_type = $3;

-- name: ListReactionsByPost :many
SELECT * FROM reactions WHERE post_id = $1 ORDER BY created_at;

-- name: CountReactionsByPost :many
SELECT reaction_type, COUNT(*) AS count FROM reactions
WHERE post_id = $1
GROUP BY reaction_type;

-- name: GetUserReactionOnPost :one
SELECT * FROM reactions WHERE user_id = $1 AND post_id = $2 AND reaction_type = $3;

-- name: AddReaction :one
INSERT INTO reactions (id, user_id, post_id, reaction_type, created_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, post_id, reaction_type) DO UPDATE SET created_at = reactions.created_at
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

-- name: CountReactionsReceivedByAuthorSince :one
SELECT COUNT(*) FROM reactions r
JOIN posts p ON p.id = r.post_id
WHERE p.author_id = $1 AND r.created_at >= $2;

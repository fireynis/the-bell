-- name: CreatePost :one
INSERT INTO posts (id, author_id, body, image_path, status, removal_reason, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetPostByID :one
SELECT * FROM posts WHERE id = $1;

-- name: ListPostsFeed :many
SELECT * FROM posts
WHERE status = 'visible' AND id < $1
ORDER BY id DESC
LIMIT $2;

-- name: ListPostsFeedFirst :many
SELECT * FROM posts
WHERE status = 'visible'
ORDER BY id DESC
LIMIT $1;

-- name: UpdatePostBody :one
UPDATE posts SET body = $2, edited_at = NOW() WHERE id = $1
RETURNING *;

-- name: SoftDeletePost :exec
UPDATE posts SET status = $2, removal_reason = $3 WHERE id = $1;

-- name: ListPostsByAuthor :many
SELECT * FROM posts WHERE author_id = $1 ORDER BY created_at DESC LIMIT $2;

-- name: CountPostsByAuthorSince :one
SELECT COUNT(*) FROM posts
WHERE author_id = $1 AND created_at >= $2 AND status = 'visible';

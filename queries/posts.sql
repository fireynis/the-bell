-- name: CreatePost :one
INSERT INTO posts (id, author_id, body, image_path, status, removal_reason, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetPostByID :one
SELECT p.id, p.author_id, p.body, p.image_path, p.status, p.removal_reason, p.created_at, p.edited_at,
       u.display_name AS author_display_name, u.avatar_url AS author_avatar_url
FROM posts p
JOIN users u ON p.author_id = u.id
WHERE p.id = $1;

-- name: ListPostsFeed :many
SELECT p.id, p.author_id, p.body, p.image_path, p.status, p.removal_reason, p.created_at, p.edited_at,
       u.display_name AS author_display_name, u.avatar_url AS author_avatar_url
FROM posts p
JOIN users u ON p.author_id = u.id
WHERE p.status = 'visible' AND p.id < $1
ORDER BY p.id DESC
LIMIT $2;

-- name: ListPostsFeedFirst :many
SELECT p.id, p.author_id, p.body, p.image_path, p.status, p.removal_reason, p.created_at, p.edited_at,
       u.display_name AS author_display_name, u.avatar_url AS author_avatar_url
FROM posts p
JOIN users u ON p.author_id = u.id
WHERE p.status = 'visible'
ORDER BY p.id DESC
LIMIT $1;

-- name: UpdatePostBody :one
UPDATE posts SET body = $2, edited_at = NOW() WHERE id = $1
RETURNING *;

-- name: SoftDeletePost :exec
UPDATE posts SET status = $2, removal_reason = $3 WHERE id = $1;

-- name: ListPostsByAuthor :many
SELECT p.id, p.author_id, p.body, p.image_path, p.status, p.removal_reason, p.created_at, p.edited_at,
       u.display_name AS author_display_name, u.avatar_url AS author_avatar_url
FROM posts p
JOIN users u ON p.author_id = u.id
WHERE p.author_id = $1
ORDER BY p.created_at DESC
LIMIT $2;

-- name: CountPostsByAuthorSince :one
SELECT COUNT(*) FROM posts
WHERE author_id = $1 AND created_at >= $2 AND status = 'visible';

-- name: CountPostsToday :one
SELECT COUNT(*) FROM posts
WHERE created_at >= CURRENT_DATE AND status = 'visible';

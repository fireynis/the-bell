-- name: CreatePost :one
INSERT INTO posts (id, author_id, body, image_path, status, removal_reason, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetPostByID :one
SELECT sqlc.embed(p),
       u.display_name AS author_display_name, u.avatar_url AS author_avatar_url
FROM posts p
JOIN users u ON p.author_id = u.id
WHERE p.id = $1;

-- name: ListPostsFeed :many
SELECT sqlc.embed(p),
       u.display_name AS author_display_name, u.avatar_url AS author_avatar_url
FROM posts p
JOIN users u ON p.author_id = u.id
WHERE p.status = 'visible' AND p.id < $1
ORDER BY p.id DESC
LIMIT $2;

-- name: ListPostsFeedFirst :many
SELECT sqlc.embed(p),
       u.display_name AS author_display_name, u.avatar_url AS author_avatar_url
FROM posts p
JOIN users u ON p.author_id = u.id
WHERE p.status = 'visible'
ORDER BY p.id DESC
LIMIT $1;

-- name: UpdatePostBody :one
-- The updated row is joined back to users so that the post returned by an edit
-- carries the same author fields as every read path. Without it the edit
-- response alone would come back with a blank author name.
UPDATE posts p SET body = $2, edited_at = NOW()
FROM users u
WHERE p.id = $1 AND u.id = p.author_id
RETURNING sqlc.embed(p),
          u.display_name AS author_display_name, u.avatar_url AS author_avatar_url;

-- name: SoftDeletePost :exec
-- Serves both takedown paths: an author deleting their own post, and a
-- moderator removing someone else's. removed_by is the moderator's id, and NULL
-- for an author deletion — there is no moderator to name.
UPDATE posts SET status = $2, removal_reason = $3, removed_by = $4 WHERE id = $1;

-- name: ListPostsByAuthor :many
-- Removed posts are excluded here for the same reason they are excluded from
-- the feed: a profile is a public listing, and a moderator-removed post carries
-- removal_reason, which is a moderator's private note.
SELECT sqlc.embed(p),
       u.display_name AS author_display_name, u.avatar_url AS author_avatar_url
FROM posts p
JOIN users u ON p.author_id = u.id
WHERE p.author_id = $1 AND p.status = 'visible'
ORDER BY p.created_at DESC
LIMIT $2;

-- name: CountPostsByAuthorSince :one
SELECT COUNT(*) FROM posts
WHERE author_id = $1 AND created_at >= $2 AND status = 'visible';

-- name: CountPostsToday :one
SELECT COUNT(*) FROM posts
WHERE created_at >= CURRENT_DATE AND status = 'visible';

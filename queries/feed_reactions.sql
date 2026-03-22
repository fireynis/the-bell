-- name: BatchCountReactionsByPosts :many
-- Counts reactions grouped by post_id and reaction_type for a batch of posts.
SELECT post_id, reaction_type, COUNT(*)::int AS count
FROM reactions
WHERE post_id = ANY(@post_ids::text[])
GROUP BY post_id, reaction_type;

-- name: BatchGetUserReactionsForPosts :many
-- Returns the current user's reactions for a batch of posts.
SELECT post_id, reaction_type
FROM reactions
WHERE user_id = @user_id AND post_id = ANY(@post_ids::text[]);

-- name: CreateTrustPenalty :one
INSERT INTO trust_penalties (id, user_id, moderation_action_id, penalty_amount, hop_depth, created_at, decays_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListTrustPenaltiesByActionIDs :many
-- Batched over a page of actions rather than read one action at a time. The
-- moderation history pairs every action it lists with the penalties that action
-- caused, so the per-action read cost one round trip per row — up to 101 for a
-- 100-action page, on both the moderator view and the member's own history.
-- The caller groups the rows by moderation_action_id; ordering by that column
-- first keeps each group's hop_depth order intact within the single result set.
SELECT * FROM trust_penalties
WHERE moderation_action_id = ANY(@action_ids::text[])
ORDER BY moderation_action_id, hop_depth ASC;

-- name: ListActivePenaltiesByUser :many
SELECT * FROM trust_penalties
WHERE user_id = $1 AND (decays_at IS NULL OR decays_at > NOW());

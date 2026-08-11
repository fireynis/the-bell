-- name: CreateModerationRelief :one
INSERT INTO moderation_reliefs (id, target_user_id, moderator_id, relief_type, previous_expires_at, was_in_force, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListMuteLiftsInForceByTarget :many
-- The member's own view of being released. was_in_force is filtered here rather
-- than after the read: the lift endpoint is idempotent and accepts a lift
-- against anyone, so filtering in Go after LIMIT would let a run of no-op lifts
-- push the release that actually freed somebody out of the window.
SELECT * FROM moderation_reliefs
WHERE target_user_id = $1
  AND relief_type = 'mute_lift'
  AND was_in_force = TRUE
ORDER BY created_at DESC
LIMIT $2;

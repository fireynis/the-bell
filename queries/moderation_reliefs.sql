-- name: CreateModerationRelief :one
INSERT INTO moderation_reliefs (id, target_user_id, moderator_id, relief_type, previous_expires_at, was_in_force, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListLiftsInForceByTarget :many
-- The member's own view of being released, one relief type at a time.
--
-- relief_type is a parameter rather than a literal because mute lifts and
-- suspension lifts are the same read against the same table — the discriminator
-- is what 00018 added the column for. A second near-identical query would be
-- free to drift from this one in exactly the way the was_in_force filter below
-- cannot afford.
--
-- was_in_force is filtered here rather than after the read: the lift endpoints
-- are idempotent and accept a lift against anyone, so filtering in Go after
-- LIMIT would let a run of no-op lifts push the release that actually freed
-- somebody out of the window.
SELECT * FROM moderation_reliefs
WHERE target_user_id = $1
  AND relief_type = $2
  AND was_in_force = TRUE
ORDER BY created_at DESC
LIMIT $3;

-- name: CreateModerationAction :one
INSERT INTO moderation_actions (id, target_user_id, moderator_id, action_type, severity, reason, duration_seconds, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListModerationActionsByTarget :many
-- Both parties are joined by name. The audit trail names who was acted on and
-- who acted, and a moderator reading it should not have to look either id up by
-- hand. Both columns are NOT NULL references to users, so neither join can drop
-- an action. Note this is the audit trail's own view: which moderator handled a
-- case stays behind the council check in the handler, exactly as it did when
-- only the id was carried.
SELECT sqlc.embed(m),
       target.display_name AS target_display_name,
       moderator.display_name AS moderator_display_name
FROM moderation_actions m
JOIN users target ON target.id = m.target_user_id
JOIN users moderator ON moderator.id = m.moderator_id
WHERE m.target_user_id = $1
ORDER BY m.created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListModerationActionsByModerator :many
-- The other direction of the same trail, joined the same way.
SELECT sqlc.embed(m),
       target.display_name AS target_display_name,
       moderator.display_name AS moderator_display_name
FROM moderation_actions m
JOIN users target ON target.id = m.target_user_id
JOIN users moderator ON moderator.id = m.moderator_id
WHERE m.moderator_id = $1
ORDER BY m.created_at DESC
LIMIT $2 OFFSET $3;

-- +goose Up
-- A suspension is temporary and must end by itself. It was enforced by
-- DeactivateUser, which sets is_active = FALSE, and no query anywhere ever set
-- that column back to TRUE — so every "7-day suspension" ever issued was in
-- fact permanent.
--
-- suspended_until is the same mechanism muted_until already uses: NULL means
-- not suspended, a future timestamp means suspended until then, and a past one
-- has simply lapsed. Nothing sweeps it, because the comparison against the
-- current time is the whole mechanism.
--
-- is_active keeps its own meaning — an account that has been taken out of
-- service — and suspension no longer touches it, so a suspension can no longer
-- write a flag only a database console could undo.
ALTER TABLE users ADD COLUMN suspended_until TIMESTAMPTZ;

-- Release the users already trapped by the old enforcement.
--
-- The action row recorded the expiry all along; only the enforcement ignored
-- it, so the length each moderator actually chose is recoverable. Suspensions
-- whose time has passed land with suspended_until in the past, which is
-- "not suspended" — that is the repair, not an oversight.
--
-- Only rows a suspension deactivated are touched: the join to the target's most
-- recent suspend action is what identifies them, so an account deactivated for
-- any other reason keeps is_active = FALSE.
UPDATE users u
SET suspended_until = latest.expires_at,
    is_active       = TRUE,
    updated_at      = NOW()
FROM (
    SELECT DISTINCT ON (target_user_id) target_user_id, expires_at
    FROM moderation_actions
    WHERE action_type = 'suspend' AND expires_at IS NOT NULL
    ORDER BY target_user_id, created_at DESC
) AS latest
WHERE u.id = latest.target_user_id
  AND u.is_active = FALSE;

-- +goose Down
-- Suspensions still in force have to go back to the only mechanism the old code
-- understands, or rolling back would silently release them.
UPDATE users
SET is_active  = FALSE,
    updated_at = NOW()
WHERE suspended_until IS NOT NULL AND suspended_until > NOW();

ALTER TABLE users DROP COLUMN IF EXISTS suspended_until;

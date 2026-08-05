-- +goose Up
-- A trust penalty is not inherently moderation-caused. Revoking a vouch costs
-- the voucher a small, decaying penalty (design doc: -3 for 30 days, to prevent
-- vouch-and-revoke gaming), and there is no moderator, action type, severity or
-- reason behind it — nobody moderated anything.
--
-- The column was NOT NULL REFERENCES moderation_actions(id), so such a penalty
-- could not be written at all: both an empty string and a synthetic id fail the
-- foreign key. The alternative was fabricating a moderation_actions row per
-- revocation, which would have to name the revoker as their own moderator
-- (something validateActionRequest rejects as self-moderation) and would put
-- invented entries into the public audit trail that the moderation UI reads.
--
-- NULL therefore means "not moderation-caused". The foreign key still applies
-- to every non-NULL value, so moderation penalties remain tied to a real action.
ALTER TABLE trust_penalties ALTER COLUMN moderation_action_id DROP NOT NULL;

-- +goose Down
-- Penalties with no moderation action cannot satisfy the restored constraint,
-- so they are removed rather than left to fail the ALTER. This loses vouch
-- revocation penalties, which is the honest cost of rolling this back.
DELETE FROM trust_penalties WHERE moderation_action_id IS NULL;
ALTER TABLE trust_penalties ALTER COLUMN moderation_action_id SET NOT NULL;

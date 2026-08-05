-- +goose Up
-- 00015 dropped NOT NULL on moderation_action_id so that a vouch revocation
-- penalty — which no moderator caused — could be written. That also removed the
-- database's guarantee that a moderation penalty is traceable to the action
-- that caused it.
--
-- This restores the half of that guarantee which can be expressed without a new
-- column. A PROPAGATED penalty (hop_depth > 0) exists only because an action
-- travelled out along the vouch graph, so one with nothing to trace back to is
-- meaningless and is now rejected.
--
-- What this deliberately does NOT catch: a DIRECT moderation penalty written
-- without an action. Those are hop_depth 0, exactly like a revocation penalty,
-- and nothing in the row distinguishes the two. Telling them apart needs a
-- discriminator column, which is more than this invariant is worth.
--
-- (The revocation window happens to be 30 days while every moderation decay is
-- 90/180/270/365 or permanent, so `decays_at - created_at` would separate them
-- today. That is a coincidence of the current constants, not an invariant:
-- adding a severity with a 30-day decay would silently start rejecting valid
-- rows, and the schema has no business depending on tunable penalty settings.)
ALTER TABLE trust_penalties
    ADD CONSTRAINT trust_penalties_propagated_needs_action
    CHECK (moderation_action_id IS NOT NULL OR hop_depth = 0);

-- +goose Down
ALTER TABLE trust_penalties
    DROP CONSTRAINT IF EXISTS trust_penalties_propagated_needs_action;

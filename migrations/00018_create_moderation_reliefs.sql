-- +goose Up
-- Non-punitive moderator acts: a moderator undoing a restriction rather than
-- imposing one. Today that is lifting a mute; unbanning and reinstating a
-- suspended account are the same shape and belong here rather than in a third
-- bespoke design, which is what relief_type is for.
--
-- Deliberately NOT a row in moderation_actions, and deliberately NOT columns on
-- one. That table's severity is CHECK (severity BETWEEN 1 AND 5) and every one
-- of those values names a real trust penalty that PropagatePenalties walks the
-- vouch graph with, so there is no severity meaning "no punishment" — a lift
-- filed there would have to claim one, recording an act of mercy as a sanction
-- against the person released and everyone who vouched for them. Same wall post
-- removal hit from the other side (see 00017).
--
-- Columns on the mute action were rejected for a different and stronger reason:
-- nothing stores which action set users.muted_until, so stamping "this action
-- was lifted" means inferring which mute is being ended. That inference has no
-- right answer when a member was muted twice in succession — the second mute
-- overwrote muted_until, but both rows look live — and no answer at all for a
-- lift issued against someone not currently muted, which the endpoint accepts
-- on purpose. A lift is a fact about the user's mute state, not about one row.
--
-- previous_expires_at is the muted_until being destroyed: the lift is the only
-- moment that value exists to be recorded, and the original action's expires_at
-- is not a substitute, because it is what the moderator first decided rather
-- than what was in force when the lift landed.
--
-- was_in_force distinguishes ending a live mute from a lift against someone who
-- was not muted. The endpoint is idempotent and answers 204 either way, so both
-- are real events, but only the first is something to tell the member about.
--
-- No explicit search_path is set here: 00001 and 00007 both reset it to public
-- as their last statement, which TestMigrations_ColumnsLandInPublicSchema
-- verifies against a real migrated database rather than by reading the SQL.
CREATE TABLE moderation_reliefs (
    id                  TEXT PRIMARY KEY,
    target_user_id      TEXT NOT NULL REFERENCES users(id),
    moderator_id        TEXT NOT NULL REFERENCES users(id),
    relief_type         TEXT NOT NULL,
    previous_expires_at TIMESTAMPTZ,
    was_in_force        BOOLEAN NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The read is always "this member's reliefs, newest first", both for the member's
-- own profile and for any later moderator view.
CREATE INDEX idx_moderation_reliefs_target ON moderation_reliefs(target_user_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS moderation_reliefs;

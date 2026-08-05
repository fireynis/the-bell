-- +goose Up
-- Moderation state must not live in a score a background worker is free to
-- recompute. A mute previously worked by dropping trust below the posting
-- threshold, which the trust worker undid within seconds of the penalty
-- landing; muted_until is authoritative and nothing recalculates it.
--
-- NULL means "not muted". A past timestamp means the mute has expired, so no
-- sweep is needed to clear it — CanPost compares against the current time.
ALTER TABLE users ADD COLUMN muted_until TIMESTAMPTZ;

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS muted_until;

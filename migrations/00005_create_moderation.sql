-- +goose Up
CREATE TABLE moderation_actions (
    id              TEXT PRIMARY KEY,
    target_user_id  TEXT NOT NULL REFERENCES users(id),
    moderator_id    TEXT NOT NULL REFERENCES users(id),
    action_type     TEXT NOT NULL,
    severity        INTEGER NOT NULL CHECK (severity BETWEEN 1 AND 5),
    reason          TEXT NOT NULL,
    duration_seconds BIGINT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ
);

CREATE INDEX idx_mod_actions_target ON moderation_actions(target_user_id);
CREATE INDEX idx_mod_actions_moderator ON moderation_actions(moderator_id);

-- Trust penalty propagation log
CREATE TABLE trust_penalties (
    id                    TEXT PRIMARY KEY,
    user_id               TEXT NOT NULL REFERENCES users(id),
    moderation_action_id  TEXT NOT NULL REFERENCES moderation_actions(id),
    penalty_amount        DOUBLE PRECISION NOT NULL,
    hop_depth             INTEGER NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decays_at             TIMESTAMPTZ
);

CREATE INDEX idx_trust_penalties_user ON trust_penalties(user_id);

-- +goose Down
DROP TABLE IF EXISTS trust_penalties;
DROP TABLE IF EXISTS moderation_actions;

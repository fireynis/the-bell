-- +goose Up
CREATE TABLE role_history (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id),
    old_role   TEXT NOT NULL,
    new_role   TEXT NOT NULL,
    reason     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_role_history_user ON role_history(user_id);
CREATE INDEX idx_role_history_created ON role_history(created_at);

ALTER TABLE users ADD COLUMN trust_below_since TIMESTAMPTZ;

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS trust_below_since;
DROP TABLE IF EXISTS role_history;

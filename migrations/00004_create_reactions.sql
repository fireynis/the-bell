-- +goose Up
CREATE TABLE reactions (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users(id),
    post_id         TEXT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    reaction_type   TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, post_id, reaction_type)
);

CREATE INDEX idx_reactions_post ON reactions(post_id);

-- +goose Down
DROP TABLE IF EXISTS reactions;

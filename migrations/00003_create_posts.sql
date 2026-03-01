-- +goose Up
CREATE TABLE posts (
    id              TEXT PRIMARY KEY,
    author_id       TEXT NOT NULL REFERENCES users(id),
    body            TEXT NOT NULL,
    image_path      TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'visible',
    removal_reason  TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    edited_at       TIMESTAMPTZ
);

CREATE INDEX idx_posts_author ON posts(author_id);
CREATE INDEX idx_posts_status_created ON posts(status, created_at DESC);
CREATE INDEX idx_posts_created ON posts(created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS posts;

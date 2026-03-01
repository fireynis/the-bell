-- +goose Up
CREATE TABLE reports (
    id          TEXT PRIMARY KEY,
    reporter_id TEXT NOT NULL REFERENCES users(id),
    post_id     TEXT NOT NULL REFERENCES posts(id),
    reason      TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_reports_status ON reports(status);
CREATE INDEX idx_reports_post ON reports(post_id);

-- +goose Down
DROP TABLE IF EXISTS reports;

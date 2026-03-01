-- +goose Up
CREATE INDEX idx_posts_status_id ON posts(status, id DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_posts_status_id;

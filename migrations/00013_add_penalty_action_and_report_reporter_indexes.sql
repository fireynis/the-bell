-- +goose Up
CREATE INDEX idx_trust_penalties_action ON trust_penalties(moderation_action_id);
CREATE INDEX idx_reports_reporter ON reports(reporter_id);

-- +goose Down
DROP INDEX IF EXISTS idx_reports_reporter;
DROP INDEX IF EXISTS idx_trust_penalties_action;

-- +goose Up
CREATE TABLE town_config (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT INTO town_config (key, value) VALUES ('bootstrap_mode', 'false');

-- +goose Down
DROP TABLE IF EXISTS town_config;

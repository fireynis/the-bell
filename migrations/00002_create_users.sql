-- +goose Up
CREATE TABLE users (
    id              TEXT PRIMARY KEY,
    kratos_identity_id TEXT NOT NULL UNIQUE,
    display_name    TEXT NOT NULL DEFAULT '',
    bio             TEXT NOT NULL DEFAULT '',
    avatar_url      TEXT NOT NULL DEFAULT '',
    trust_score     DOUBLE PRECISION NOT NULL DEFAULT 50.0,
    role            TEXT NOT NULL DEFAULT 'pending',
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    joined_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_users_kratos_id ON users(kratos_identity_id);
CREATE INDEX idx_users_role ON users(role);
CREATE INDEX idx_users_trust_score ON users(trust_score);

-- +goose Down
DROP TABLE IF EXISTS users;

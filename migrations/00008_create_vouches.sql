-- +goose Up
CREATE TABLE vouches (
    id          TEXT PRIMARY KEY,
    voucher_id  TEXT NOT NULL REFERENCES users(id),
    vouchee_id  TEXT NOT NULL REFERENCES users(id),
    status      TEXT NOT NULL DEFAULT 'active',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at  TIMESTAMPTZ,
    CONSTRAINT uq_voucher_vouchee UNIQUE (voucher_id, vouchee_id),
    CONSTRAINT chk_no_self_vouch CHECK (voucher_id != vouchee_id)
);

CREATE INDEX idx_vouches_vouchee_status ON vouches(vouchee_id, status);
CREATE INDEX idx_vouches_voucher_status ON vouches(voucher_id, status);

-- +goose Down
DROP TABLE IF EXISTS vouches;

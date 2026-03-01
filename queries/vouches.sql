-- name: CreateVouch :one
INSERT INTO vouches (id, voucher_id, vouchee_id, status, created_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetVouchByID :one
SELECT * FROM vouches WHERE id = $1;

-- name: GetVouchByPair :one
SELECT * FROM vouches WHERE voucher_id = $1 AND vouchee_id = $2;

-- name: ListActiveVouchesByVouchee :many
SELECT * FROM vouches
WHERE vouchee_id = $1 AND status = 'active'
ORDER BY created_at;

-- name: ListActiveVouchesByVoucher :many
SELECT * FROM vouches
WHERE voucher_id = $1 AND status = 'active'
ORDER BY created_at;

-- name: RevokeVouch :one
UPDATE vouches
SET status = 'revoked', revoked_at = NOW()
WHERE id = $1 AND status = 'active'
RETURNING *;

-- name: CountVouchesByVoucherSince :one
SELECT COUNT(*) FROM vouches
WHERE voucher_id = $1 AND created_at >= $2;

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

-- name: CountActiveModeratorVouchesForUser :one
SELECT COUNT(*) FROM vouches v
JOIN users u ON u.id = v.voucher_id
WHERE v.vouchee_id = $1
  AND v.status = 'active'
  AND u.role IN ('moderator', 'council')
  AND u.is_active = TRUE;

-- name: CountActiveVouchesWithAvgTrust :one
SELECT COUNT(*) AS vouch_count, COALESCE(AVG(u.trust_score), 0)::double precision AS avg_trust
FROM vouches v
JOIN users u ON u.id = v.voucher_id
WHERE v.vouchee_id = $1 AND v.status = 'active';

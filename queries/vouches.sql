-- name: CreateVouch :one
INSERT INTO vouches (id, voucher_id, vouchee_id, status, created_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetVouchByID :one
SELECT * FROM vouches WHERE id = $1;

-- name: GetVouchByPair :one
SELECT * FROM vouches WHERE voucher_id = $1 AND vouchee_id = $2;

-- name: ListActiveVouchesByVouchee :many
-- Both parties are joined by name because a vouch list is about people, and the
-- id alone is unreadable: the profile rendered "0193a7b2..." for every row.
-- Inner joins are safe on both sides — voucher_id and vouchee_id are NOT NULL
-- references to users — so no row can be dropped by the join. Following the
-- feed's convention (posts.sql), the name travels with the row rather than
-- being fetched per id afterwards.
SELECT sqlc.embed(v),
       voucher.display_name AS voucher_display_name,
       vouchee.display_name AS vouchee_display_name
FROM vouches v
JOIN users voucher ON voucher.id = v.voucher_id
JOIN users vouchee ON vouchee.id = v.vouchee_id
WHERE v.vouchee_id = $1 AND v.status = 'active'
ORDER BY v.created_at;

-- name: ListActiveVouchesByVoucher :many
-- The other direction of the same list, joined the same way.
SELECT sqlc.embed(v),
       voucher.display_name AS voucher_display_name,
       vouchee.display_name AS vouchee_display_name
FROM vouches v
JOIN users voucher ON voucher.id = v.voucher_id
JOIN users vouchee ON vouchee.id = v.vouchee_id
WHERE v.voucher_id = $1 AND v.status = 'active'
ORDER BY v.created_at;

-- name: RevokeVouch :one
UPDATE vouches
SET status = 'revoked', revoked_at = NOW()
WHERE id = $1 AND status = 'active'
RETURNING *;

-- name: CountVouchesByVoucherSince :one
SELECT COUNT(*) FROM vouches
WHERE voucher_id = $1 AND created_at >= $2;

-- name: CountActiveModeratorVouchesForUser :one
-- The suspension clause pairs with is_active for the reason spelled out at the
-- top of users.sql: a suspension is an expiry rather than a flag, so asking
-- only `is_active = TRUE` would let a suspended moderator's vouch carry someone
-- to promotion, and dropping the pair would keep discounting it long after the
-- suspension lapsed.
SELECT COUNT(*) FROM vouches v
JOIN users u ON u.id = v.voucher_id
WHERE v.vouchee_id = $1
  AND v.status = 'active'
  AND u.role IN ('moderator', 'council')
  AND u.is_active = TRUE
  AND (u.suspended_until IS NULL OR u.suspended_until <= NOW());

-- name: CountActiveVouchesWithAvgTrust :one
SELECT COUNT(*) AS vouch_count, COALESCE(AVG(u.trust_score), 0)::double precision AS avg_trust
FROM vouches v
JOIN users u ON u.id = v.voucher_id
WHERE v.vouchee_id = $1 AND v.status = 'active';

-- name: CreateVouch :one
INSERT INTO vouches (id, voucher_id, vouchee_id, status, created_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetVouchByID :one
SELECT * FROM vouches WHERE id = $1;

-- name: GetVouchByPair :one
-- Deliberately unfiltered by status. A pair has at most one row —
-- UNIQUE(voucher_id, vouchee_id) — so this is the whole history of the pair,
-- and the service needs the revoked row as much as the active one: an active
-- one is a duplicate to refuse, a revoked one is the row ReactivateVouch
-- brings back.
SELECT * FROM vouches WHERE voucher_id = $1 AND vouchee_id = $2;

-- name: ReactivateVouch :one
-- Vouching again for someone you previously revoked reuses that row rather
-- than inserting a second one, because the unique constraint on the pair means
-- there can never be a second one to insert.
--
-- created_at moves to the new vouch's timestamp so the row reads as the
-- endorsement it now is: the daily limit counts from created_at, so leaving it
-- at the original date would let a member revoke and re-vouch without ever
-- spending an allowance. revoked_at is cleared for the same reason — a
-- revocation that has been undone must leave no trace that ListActiveVouches*
-- or the trust calculation could still read.
--
-- The status guard makes this a no-op on an already-active row, so a caller
-- that reached here on a live vouch gets no rows rather than silently
-- refreshing somebody's vouch date.
UPDATE vouches
SET status = 'active', created_at = $2, revoked_at = NULL
WHERE id = $1 AND status = 'revoked'
RETURNING *;

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

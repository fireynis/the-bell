-- name: CreateInvite :one
INSERT INTO invites (id, token_hash, email, note, inviter_id, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetLiveInviteByTokenHash :one
-- The registration gate's and the public lookup's only read.
--
-- "Live" is spelled out here rather than derived in Go so that an invitation
-- that has been consumed, revoked or has simply run out is indistinguishable
-- from one that never existed: every caller of this query answers a miss with
-- the same 404, and a query that returned the row and let the service judge it
-- would make that uniformity one forgotten branch away from an oracle.
SELECT sqlc.embed(i), inviter.display_name AS inviter_display_name
FROM invites i
JOIN users inviter ON inviter.id = i.inviter_id
WHERE i.token_hash = $1
  AND i.consumed_at IS NULL
  AND i.revoked_at IS NULL
  AND i.expires_at > sqlc.arg(now);

-- name: GetLiveInviteByEmail :one
-- The redemption path's read: the invitation waiting for whoever just signed in
-- with this address. Same liveness clause as the token lookup.
SELECT sqlc.embed(i), inviter.display_name AS inviter_display_name
FROM invites i
JOIN users inviter ON inviter.id = i.inviter_id
WHERE lower(i.email) = lower(sqlc.arg(email)::text)
  AND i.consumed_at IS NULL
  AND i.revoked_at IS NULL
  AND i.expires_at > sqlc.arg(now);

-- name: GetBlockingInviteByEmail :one
-- The row idx_invites_live_email would collide with: unconsumed and unrevoked,
-- whether or not it has expired.
--
-- This is deliberately NOT the liveness clause above. The unique index cannot
-- test expiry — an index predicate has to be immutable — so an expired
-- invitation still occupies the address as far as the constraint is concerned.
-- Create reads this row to tell the two cases apart: a live one refuses the
-- request, an expired one is reaped and replaced.
SELECT * FROM invites
WHERE lower(email) = lower(sqlc.arg(email)::text)
  AND consumed_at IS NULL
  AND revoked_at IS NULL;

-- name: CountConsumedInvitesByEmail :one
-- Whether this address has already been through the door. See
-- InviteService.Create for why an accepted invitation refuses a new one and why
-- this app-side check is where the duplicate-account question stops.
SELECT COUNT(*) FROM invites
WHERE lower(email) = lower(sqlc.arg(email)::text) AND consumed_at IS NOT NULL;

-- name: CountInvitesByInviterSince :one
-- Half of the combined daily budget; CountVouchesByVoucherSince is the other.
-- Revoked invitations are counted on purpose: the point of the budget is to
-- limit how many endorsements one member can put into the world in a day, and
-- inviting-then-revoking in a loop must not be a way to buy more.
SELECT COUNT(*) FROM invites
WHERE inviter_id = $1 AND created_at >= sqlc.arg(since);

-- name: ListInvitesByInviter :many
-- The caller's own invitations, newest first.
--
-- consumed_by is joined by name because the list is read by a person: "accepted
-- by Dana" is the whole reason the row is interesting, and the id renders as
-- unreadable UUID. A LEFT JOIN because consumed_by is null on every invitation
-- that has not been accepted, which is most of them.
--
-- token_hash is selected — this is SELECT i.* — and the repository drops it
-- before the row leaves the persistence layer. domain.Invite has nowhere to put
-- it.
SELECT sqlc.embed(i), consumer.display_name AS consumed_by_display_name
FROM invites i
LEFT JOIN users consumer ON consumer.id = i.consumed_by
WHERE i.inviter_id = $1
ORDER BY i.created_at DESC;

-- name: RevokeInviteByInviter :one
-- Withdrawing an invitation you sent, and only one that is still open.
--
-- Both the ownership test and the liveness clause are in the WHERE rather than
-- checked in Go against a row read first. Zero rows is then the single answer
-- to "not yours", "already accepted", "already revoked" and "no such
-- invitation", which is exactly the uniformity the handler needs to 404 without
-- telling a caller whether somebody else's invitation exists.
UPDATE invites
SET revoked_at = sqlc.arg(revoked_at)
WHERE id = $1
  AND inviter_id = sqlc.arg(inviter_id)
  AND consumed_at IS NULL
  AND revoked_at IS NULL
  AND expires_at > sqlc.arg(now)
RETURNING *;

-- name: ReapInviteForReuse :exec
-- Frees an address held by an expired invitation, so a fresh one can be sent.
--
-- Unqualified by expiry on purpose: Create has already established that this
-- row is past its expires_at, and re-testing it here against a second clock
-- reading would open a window where neither statement thinks it is responsible.
-- The consumed/revoked guards stay, so a row that was accepted or withdrawn in
-- the meantime is left exactly as it is and the INSERT that follows loses to
-- the unique index instead — which is the right answer, because in that case
-- the address is legitimately taken.
UPDATE invites
SET revoked_at = sqlc.arg(revoked_at)
WHERE id = $1 AND consumed_at IS NULL AND revoked_at IS NULL;

-- name: ConsumeInvite :one
-- Redemption, exactly once.
--
-- The liveness clause is repeated here rather than trusted from the read that
-- selected this row, and that repetition is the whole guard: two concurrent
-- sign-ins with the same address both find the invitation live, both arrive
-- here, and only one updates a row. The loser gets no rows back and creates no
-- vouch, so an invitation can never produce two endorsements.
UPDATE invites
SET consumed_at = sqlc.arg(consumed_at), consumed_by = sqlc.arg(consumed_by)
WHERE id = $1
  AND consumed_at IS NULL
  AND revoked_at IS NULL
  AND expires_at > sqlc.arg(consumed_at)
RETURNING *;

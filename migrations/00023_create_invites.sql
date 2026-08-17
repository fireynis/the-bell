-- +goose Up
-- Invitations, which are how people join an invite-only town.
--
-- An invite IS a vouch that has not landed yet. A member who could vouch for
-- somebody may instead invite them by email, and accepting the invitation
-- creates that vouch — so the row below is an endorsement in escrow, spending
-- the same daily budget a vouch does and re-checked against the same trust rule
-- at the moment it is redeemed.
--
-- token_hash holds the SHA-256 hex of the raw token, never the token itself.
-- The raw value is returned exactly once, in the response to the request that
-- created it, and after that it exists only in the invitee's inbox. A database
-- dump therefore cannot be replayed into accounts: the hash cannot be presented
-- at the registration gate. UNIQUE on the hash is both the lookup index and the
-- guarantee that two invites cannot share a token.
--
-- email is stored as typed but matched case-insensitively everywhere (see the
-- partial index below and the queries in queries/invites.sql). Addresses are
-- case-insensitive in every practical mail system, and an invitation that
-- refused to redeem because the invitee's client capitalised their own name
-- would be indistinguishable from a broken link.
--
-- consumed_at/consumed_by and revoked_at are set-once terminal markers rather
-- than a status column. Deriving the status at read time is what lets an
-- invitation expire without anything having to run: nothing sweeps this table,
-- and an expiry that depended on a sweep would leave every invite live on a
-- deployment whose scheduler stopped.
CREATE TABLE invites (
    id          TEXT PRIMARY KEY,
    token_hash  TEXT NOT NULL UNIQUE,
    email       TEXT NOT NULL,
    note        TEXT NOT NULL DEFAULT '',
    inviter_id  TEXT NOT NULL REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    consumed_by TEXT REFERENCES users(id),
    revoked_at  TIMESTAMPTZ
);

-- One unconsumed, unrevoked invitation per address.
--
-- The predicate deliberately says nothing about expiry, and it cannot: an index
-- predicate must be immutable, so now() is not available to it. The consequence
-- has to be handled a layer up, and it is — InviteService.Create treats an
-- expired row as not live, and REVOKES it before inserting the replacement, so
-- expiry does free the address without this index ever seeing two live rows for
-- it. Reaping that way (rather than deleting) keeps the history, and because
-- the reap stamps revoked_at at a time already past expires_at, the row still
-- reads as "expired" rather than "revoked" to the inviter — see
-- domain.Invite.Status.
--
-- The index is what makes the rule safe under concurrency. Two simultaneous
-- invitations to the same address both pass the service's liveness check and
-- one of them then loses to this constraint, which the repository maps to a
-- validation error. Without it the winner would be decided by nothing.
CREATE UNIQUE INDEX idx_invites_live_email
    ON invites (lower(email))
    WHERE consumed_at IS NULL AND revoked_at IS NULL;

-- The inviter's own list, newest first — the only listing the API serves.
CREATE INDEX idx_invites_inviter_created ON invites (inviter_id, created_at DESC);

-- Invite-only is the default, including for towns upgrading into it.
--
-- ON CONFLICT DO NOTHING so that re-running this against a town that has
-- already chosen a mode leaves that choice alone. A town that wants the old
-- behaviour sets registration_mode to 'open' from the council's config screen;
-- the deliberate part is that it has to be chosen, because the alternative —
-- defaulting existing towns to 'open' — would leave a town that upgraded for
-- this feature still accepting strangers until somebody noticed.
INSERT INTO town_config (key, value) VALUES ('registration_mode', 'invite')
ON CONFLICT (key) DO NOTHING;

-- +goose Down
DELETE FROM town_config WHERE key = 'registration_mode';
DROP TABLE IF EXISTS invites;

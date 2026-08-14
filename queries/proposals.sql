-- name: CreateProposal :one
INSERT INTO proposals (id, type, target_user_id, rationale, created_by, status, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetProposal :one
SELECT * FROM proposals WHERE id = $1;

-- Both listings join the target and the proposer to their display names.
--
-- A LEFT JOIN for the target because bootstrap_reentry has none, and a LEFT
-- JOIN for the proposer too even though created_by is NOT NULL: an INNER JOIN
-- there would make a proposal vanish from the council's queue if the row it
-- points at ever became unreadable, and a motion disappearing is a worse
-- failure than one attributed to a blank name. Both names come back as the
-- empty string when absent, matching the vouch and directory listings, so a
-- client falls back to the id for anything falsy.

-- name: ListOpenProposals :many
SELECT sqlc.embed(p),
       COALESCE(t.display_name, '')::text AS target_display_name,
       COALESCE(c.display_name, '')::text AS created_by_display_name
FROM proposals p
LEFT JOIN users t ON t.id = p.target_user_id
LEFT JOIN users c ON c.id = p.created_by
WHERE p.status = 'open'
ORDER BY p.created_at DESC;

-- name: ListDecidedProposals :many
-- Newest decision first, and bounded: this is the council's record of what it
-- has settled, not an archive to be paged through, so the API asks for a page
-- rather than the whole history of the town.
SELECT sqlc.embed(p),
       COALESCE(t.display_name, '')::text AS target_display_name,
       COALESCE(c.display_name, '')::text AS created_by_display_name
FROM proposals p
LEFT JOIN users t ON t.id = p.target_user_id
LEFT JOIN users c ON c.id = p.created_by
WHERE p.status <> 'open'
ORDER BY p.decided_at DESC NULLS LAST, p.created_at DESC
LIMIT @row_limit::int;

-- name: FindOpenProposalByTypeAndTarget :one
-- The readable half of the partial unique index in 00021. The index is what
-- actually guarantees one open proposal per (type, target); this exists so the
-- council is told "there is already an open motion about this" instead of
-- receiving a constraint violation.
--
-- COALESCE on both sides so the bootstrap_reentry case — target NULL — matches
-- itself, which `target_user_id = $2` would not: NULL = NULL is unknown.
SELECT * FROM proposals
WHERE type = $1
  AND COALESCE(target_user_id, '') = COALESCE(sqlc.narg(target_user_id)::text, '')
  AND status = 'open';

-- name: DecideProposal :one
-- Flips a proposal out of 'open' and stamps when. The status guard in the WHERE
-- clause makes this a no-op on an already-decided proposal rather than a second
-- decision overwriting the first, and returning no row is how the caller learns
-- somebody else decided it first.
UPDATE proposals
SET status = $2, decided_at = $3
WHERE id = $1 AND status = 'open'
RETURNING *;

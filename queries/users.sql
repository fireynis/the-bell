-- Every query below that asks for the active users means "active and not
-- currently serving a suspension". A suspension is a value with an expiry
-- (suspended_until) rather than a flag, so the clock is part of the question:
-- `is_active = TRUE` alone would count a suspended member among the members,
-- and dropping the pair would leave one still counted a day after their
-- suspension lapsed. Postgres NOW() is the authority, matching
-- domain.User.IsSuspended on the read side.

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByKratosID :one
SELECT * FROM users WHERE kratos_identity_id = $1;

-- name: CreateUser :one
INSERT INTO users (id, kratos_identity_id, display_name, bio, avatar_url, trust_score, role, is_active, joined_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: UpdateUserProfile :one
UPDATE users
SET display_name = $2, bio = $3, avatar_url = $4, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateUserTrustScore :exec
UPDATE users SET trust_score = $2, updated_at = NOW() WHERE id = $1;

-- name: UpdateUserRole :exec
UPDATE users SET role = $2, updated_at = NOW() WHERE id = $1;

-- name: ListPendingUsers :many
SELECT * FROM users
WHERE role = 'pending' AND is_active = TRUE
  AND (suspended_until IS NULL OR suspended_until <= NOW())
ORDER BY created_at ASC;

-- The council's approval queue, one page at a time. ListPendingUsers above is
-- the whole roster and stays: the display-name backfill walks every applicant
-- and has no page to be on.
--
-- Oldest first, which is the fair order for a queue: the applicant who has been
-- waiting longest is the one the council sees first, and a registration flood
-- cannot bury somebody who signed up last week behind fifty newer strangers.
-- It is the opposite of the member directory, which is newest-first because it
-- answers a different question — who has just arrived and needs a vouch.
--
-- joined_at rather than created_at, which the unpaged listing sorts on, because
-- joined_at is the date the queue shows next to each name: an order the council
-- cannot verify against what is on screen invites a bug report every time the
-- two columns drift. id breaks ties, so paging with an offset cannot repeat or
-- skip an applicant when two accounts share a timestamp.
--
-- An empty @query matches everyone; otherwise it is a case-insensitive
-- substring of the display name, escaped by the caller exactly as the directory
-- escapes it.

-- name: ListPendingUsersPage :many
SELECT * FROM users
WHERE role = 'pending' AND is_active = TRUE
  AND (suspended_until IS NULL OR suspended_until <= NOW())
  AND (@query::text = '' OR display_name ILIKE '%' || @query::text || '%')
ORDER BY joined_at ASC, id ASC
LIMIT @row_limit::int OFFSET @row_offset::int;

-- name: CountPendingUsersMatching :one
-- The same population as ListPendingUsersPage, so the two filters must stay
-- identical: a total that disagrees with the rows is a pager offering a page
-- that comes back empty. With an empty @query it counts exactly what
-- CountPendingUsers counts, which is what keeps the queue's total and the
-- dashboard's pending stat from contradicting each other.
SELECT COUNT(*) FROM users
WHERE role = 'pending' AND is_active = TRUE
  AND (suspended_until IS NULL OR suspended_until <= NOW())
  AND (@query::text = '' OR display_name ILIKE '%' || @query::text || '%');

-- name: CountUsersByMinRole :one
SELECT COUNT(*) FROM users
WHERE role IN ('member', 'moderator', 'council') AND is_active = TRUE
  AND (suspended_until IS NULL OR suspended_until <= NOW());

-- name: DeactivateUser :exec
-- Takes an account out of service indefinitely. Suspension no longer uses this:
-- nothing sets is_active back to TRUE, which is what made every timed
-- suspension permanent. Use SetUserSuspendedUntil for anything that ends.
UPDATE users SET is_active = FALSE, updated_at = NOW() WHERE id = $1;

-- name: SetUserMutedUntil :exec
-- Passing NULL lifts the mute. Setting it always overwrites rather than
-- extending, so a moderator issuing a shorter mute over a longer one gets the
-- length they chose.
UPDATE users SET muted_until = $2, updated_at = NOW() WHERE id = $1;

-- name: SetUserSuspendedUntil :exec
-- The same contract as SetUserMutedUntil one severity up: NULL lifts the
-- suspension, and a new one overwrites rather than extends.
UPDATE users SET suspended_until = $2, updated_at = NOW() WHERE id = $1;

-- name: SetUserResidencyClaim :exec
-- The resident's own statement of where they live, for the council's approval
-- queue. Writing the empty string clears it.
--
-- residency_claim_updated_at is stamped on every write including a clear, so
-- "withdrew their claim last week" stays distinguishable from "never made one",
-- which is the whole reason the column is nullable while the claim is not.
UPDATE users
SET residency_claim            = $2,
    residency_claim_updated_at = NOW(),
    updated_at                 = NOW()
WHERE id = $1;

-- name: CountCouncilMembers :one
SELECT COUNT(*) FROM users
WHERE role = 'council' AND is_active = TRUE
  AND (suspended_until IS NULL OR suspended_until <= NOW());

-- name: ListActiveNonBannedUsers :many
SELECT * FROM users
WHERE is_active = TRUE AND role NOT IN ('pending', 'banned')
  AND (suspended_until IS NULL OR suspended_until <= NOW())
ORDER BY created_at;

-- name: UpdateUserTrustBelowSince :exec
UPDATE users SET trust_below_since = $2, updated_at = NOW() WHERE id = $1;

-- name: ClearUserTrustBelowSince :exec
UPDATE users SET trust_below_since = NULL, updated_at = NOW() WHERE id = $1;

-- name: CountAllUsers :one
SELECT COUNT(*) FROM users
WHERE is_active = TRUE
  AND (suspended_until IS NULL OR suspended_until <= NOW());

-- name: CountModerators :one
SELECT COUNT(*) FROM users
WHERE role = 'moderator' AND is_active = TRUE
  AND (suspended_until IS NULL OR suspended_until <= NOW());

-- name: CountPendingUsers :one
SELECT COUNT(*) FROM users
WHERE role = 'pending' AND is_active = TRUE
  AND (suspended_until IS NULL OR suspended_until <= NOW());

-- The member directory. Pending users cannot post, so before this existed they
-- were invisible to everyone who might vouch for them — the vouch graph had no
-- way to start. It therefore includes pending accounts deliberately: being
-- findable is the whole point.
--
-- Banned and deactivated accounts are excluded, as is anyone currently serving
-- a suspension, on the same clock as every other query in this file. An empty
-- @query matches everyone; otherwise it is a case-insensitive substring of the
-- display name. The caller escapes LIKE's own metacharacters before it gets
-- here, so a resident searching for "_" finds an underscore rather than every
-- neighbour.
--
-- Newest first, because the residents most likely to need a vouch are the ones
-- who just arrived. id breaks ties: joined_at can repeat, and two rows in an
-- ambiguous order would shuffle between pages of an offset-paginated listing.

-- name: ListDirectoryUsers :many
SELECT * FROM users
WHERE is_active = TRUE AND role <> 'banned'
  AND (suspended_until IS NULL OR suspended_until <= NOW())
  AND (@query::text = '' OR display_name ILIKE '%' || @query::text || '%')
ORDER BY joined_at DESC, id DESC
LIMIT @row_limit::int OFFSET @row_offset::int;

-- name: CountDirectoryUsers :one
-- The same population as ListDirectoryUsers, so the two filters must stay
-- identical: a total that disagrees with the rows is a pager that offers a page
-- which comes back empty.
SELECT COUNT(*) FROM users
WHERE is_active = TRUE AND role <> 'banned'
  AND (suspended_until IS NULL OR suspended_until <= NOW())
  AND (@query::text = '' OR display_name ILIKE '%' || @query::text || '%');

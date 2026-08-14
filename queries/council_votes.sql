-- name: CreateCouncilVote :one
INSERT INTO council_votes (id, proposal_id, voter_id, vote, created_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- ListVotesByProposal is what every tally is computed from. Approvals,
-- rejections and whether the caller has already voted all come off this one
-- read, because a proposal's votes are bounded by the size of the council —
-- a handful of rows. It replaced a pair of COUNT-per-side queries plus a
-- targeted GetVoteByProposalAndVoter, which was three round trips to answer
-- three questions about the same few rows.
--
-- There is deliberately no query listing proposals out of this table any more.
-- ListDistinctOpenProposals used to reconstruct the set of proposals from the
-- votes cast on them, which was the only way to enumerate them while no
-- proposals table existed — it could not see a proposal nobody had voted on
-- yet, and it called every proposal it found "open" regardless of outcome.
-- Migration 00021 gives proposals their own table; ask that.

-- name: ListVotesByProposal :many
SELECT * FROM council_votes
WHERE proposal_id = $1
ORDER BY created_at ASC;

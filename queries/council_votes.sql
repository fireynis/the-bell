-- name: CreateCouncilVote :one
INSERT INTO council_votes (id, proposal_id, voter_id, vote, created_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetVoteByProposalAndVoter :one
SELECT * FROM council_votes
WHERE proposal_id = $1 AND voter_id = $2;

-- name: ListVotesByProposal :many
SELECT * FROM council_votes
WHERE proposal_id = $1
ORDER BY created_at ASC;

-- name: CountVotesByProposalAndVote :one
SELECT COUNT(*) FROM council_votes
WHERE proposal_id = $1 AND vote = $2;

-- name: ListDistinctOpenProposals :many
SELECT DISTINCT proposal_id FROM council_votes
ORDER BY proposal_id;

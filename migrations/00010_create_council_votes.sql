-- +goose Up
CREATE TABLE council_votes (
    id          TEXT PRIMARY KEY,
    proposal_id TEXT NOT NULL,
    voter_id    TEXT NOT NULL REFERENCES users(id),
    vote        TEXT NOT NULL CHECK (vote IN ('approve', 'reject')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_council_votes_proposal_voter ON council_votes(proposal_id, voter_id);
CREATE INDEX idx_council_votes_proposal ON council_votes(proposal_id);

-- +goose Down
DROP TABLE IF EXISTS council_votes;

-- +goose Up
-- The thing the council actually votes on.
--
-- council_votes has existed since 00010 with a proposal_id column and no
-- proposals table behind it. Nothing ever created a proposal, nothing ever read
-- a tally back, and no outcome was ever acted on: the whole voting system was a
-- shell that recorded opinions about identifiers that referred to nothing. This
-- table is the missing half, and the FK below is what makes proposal_id mean
-- something.
--
-- Three types, and the list is closed by a CHECK because each one names a
-- specific thing the service EXECUTES when the vote carries. A type the code
-- cannot execute is a proposal the council can pass and watch do nothing, which
-- is the failure this table exists to end:
--
--   council_promotion  — a moderator joins the council.
--   council_removal    — a council member returns to being a member. The target
--                        does not vote on their own removal, so the electorate
--                        for this type is the council minus one.
--   bootstrap_reentry  — the town goes back into bootstrap mode, where the
--                        council admits residents directly. This is the
--                        reversal that was missing: leaving bootstrap mode was
--                        a one-way door, so a town that grew past the exit
--                        threshold and then shrank had no way back to the only
--                        mechanism that lets people in without a vouch.
--
-- target_user_id is nullable because bootstrap_reentry is about the town rather
-- than a person. The service requires a target for the other two and refuses
-- one here, which the schema cannot express and does not try to.
--
-- status is open/passed/rejected rather than the pending/approved/rejected the
-- old shell used in Go. "passed" and "rejected" are what a town says about a
-- motion, and "open" is the state a member of the public can see it in.
-- decided_at is set exactly when status leaves 'open'.
CREATE TABLE proposals (
    id             TEXT PRIMARY KEY,
    type           TEXT NOT NULL CHECK (type IN ('council_promotion', 'council_removal', 'bootstrap_reentry')),
    target_user_id TEXT REFERENCES users(id),
    rationale      TEXT NOT NULL,
    created_by     TEXT NOT NULL REFERENCES users(id),
    status         TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'passed', 'rejected')),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_at     TIMESTAMPTZ
);

-- One open proposal per (type, target) at a time. Two open motions to promote
-- the same moderator would split the council's votes between them and neither
-- would reach a majority, so this is a correctness constraint rather than
-- tidiness.
--
-- COALESCE, not the bare column: a unique index treats NULLs as distinct, so
-- indexing target_user_id directly would constrain the two targeted types and
-- silently exempt bootstrap_reentry — the one type whose target is always NULL,
-- and therefore the one where every proposal would collide if the index worked
-- and none do if it does not. The empty string is not a valid users.id, so
-- collapsing NULL onto it cannot merge a real target with the town-wide case.
--
-- Partial on status: a decided proposal is history and must not block the
-- council from raising the same question again next month.
CREATE UNIQUE INDEX idx_proposals_open_type_target
    ON proposals (type, COALESCE(target_user_id, '')) WHERE status = 'open';

-- The two listings the API serves: open proposals, and decided ones newest
-- first.
CREATE INDEX idx_proposals_status_created ON proposals (status, created_at DESC);

-- Now make proposal_id refer to something.
--
-- Existing council_votes rows hold arbitrary strings — whatever the old shell's
-- callers passed as a proposal id — and no proposals row exists for any of
-- them, because until this migration none could. Adding the constraint would
-- fail against any such row, so they are deleted first.
--
-- Deleting votes is normally unthinkable, and it is defensible here only
-- because of what those votes are: opinions recorded against identifiers that
-- referred to nothing, on motions that had no text, no proposer and no outcome,
-- which no code path ever tallied or acted on. There is no decision to
-- invalidate because no decision was ever reachable. Keeping them would mean
-- either leaving proposal_id unconstrained forever or inventing placeholder
-- proposals whose type and rationale would be fabricated.
--
-- The delete is written against the new table rather than as an unqualified
-- TRUNCATE so it stays correct if this migration is ever run after proposals
-- have been created — it removes orphans, and today every row is one.
DELETE FROM council_votes cv
WHERE NOT EXISTS (SELECT 1 FROM proposals p WHERE p.id = cv.proposal_id);

ALTER TABLE council_votes
    ADD CONSTRAINT council_votes_proposal_id_fkey
    FOREIGN KEY (proposal_id) REFERENCES proposals(id);

-- +goose Down
-- The column shape goes back; the deleted rows do not. They cannot: they were
-- deleted precisely because nothing recorded what they referred to, so there is
-- nothing to restore them from. Rolling back returns council_votes to an
-- unconstrained proposal_id, which is the shape the old shell expected.
ALTER TABLE council_votes DROP CONSTRAINT IF EXISTS council_votes_proposal_id_fkey;
DROP TABLE IF EXISTS proposals;

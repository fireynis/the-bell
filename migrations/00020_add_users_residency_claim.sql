-- +goose Up
-- Where a resident says they live, in their own words, for the council to read
-- while deciding whether to approve them.
--
-- It is a claim, not a verified fact, and the column name says so. Nothing in
-- this codebase checks it against anything: no address service, no postcode
-- table, no document upload. A council member reads the string, recognises the
-- street or does not, and approves or does not — and the approval is already
-- recorded against the council member who made it, so the platform's record is
-- "this person said X and that person believed them", which is the honest one.
-- How hard to press on a claim is each town's own business; a village where
-- everyone knows everyone needs less than a commuter suburb, and encoding one
-- town's rigor in the schema would impose it on every other.
--
-- Deliberately a column on users rather than a Kratos trait. Traits are
-- identity data the auth provider owns and the registration form collects once;
-- this is app data the resident edits afterwards, the council reads, and the
-- approval queue joins against. Putting it in Kratos would mean every read of
-- the approval queue fanned out to the identity API, and would put an
-- unverified free-text address in the identity store, where it would ride along
-- in session payloads to no purpose.
--
-- NOT NULL DEFAULT '' rather than nullable: "has not said" and "said nothing"
-- are the same state to every reader, and a nullable column would make every
-- one of them handle both spellings of it. Clearing a claim writes the empty
-- string.
ALTER TABLE users ADD COLUMN residency_claim TEXT NOT NULL DEFAULT '';

-- When the claim was last written, including when it was cleared. NULL means
-- the resident has never touched it, which is the one thing the empty string
-- above cannot distinguish — a claim withdrawn last week is a different fact
-- from one never made, and only the council reviewing the queue can tell what
-- to do with the difference.
ALTER TABLE users ADD COLUMN residency_claim_updated_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS residency_claim_updated_at;
ALTER TABLE users DROP COLUMN IF EXISTS residency_claim;

-- +goose Up
-- removal_reason has recorded WHY a post was taken down since 00003, but until
-- moderator removal existed nothing ever wrote one, so nothing recorded WHO.
-- "A moderator removed this, and here is their note" is not an audit trail if
-- it cannot name the moderator, and adding the column after removals are live
-- would strand every row written in between as permanently unattributable.
--
-- Nullable, with no default and no backfill. An author deleting their own post
-- has no moderator, and neither does any row predating this migration; NULL is
-- the honest value for both, and inventing one would be worse than the gap.
--
-- Deliberately NOT tied to moderation_actions. Every row in that table flows
-- through PropagatePenalties, which walks the vouch graph and docks trust from
-- the author's vouchers — so modelling a post removal as a moderation action
-- would penalise everyone who ever vouched for the author. That table means
-- "punishment applied to a person"; this column records an action against a
-- piece of content. A moderator who judges the person to be the problem still
-- has warn/mute/suspend/ban, chosen deliberately.
--
-- No explicit search_path is set here: 00001 and 00007 both reset it to public
-- as their last statement, which TestMigrations_ColumnsLandInPublicSchema
-- verifies against a real migrated database rather than by reading the SQL.
ALTER TABLE posts ADD COLUMN removed_by TEXT REFERENCES users(id);

-- +goose Down
ALTER TABLE posts DROP COLUMN IF EXISTS removed_by;

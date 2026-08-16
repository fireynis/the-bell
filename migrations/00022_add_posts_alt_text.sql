-- +goose Up
-- What is in a post's image, in the author's own words, for a neighbour who
-- cannot see it.
--
-- Every image in the feed used to reach a screen reader as alt="" — announced
-- as nothing at all, because the card had no description to give and correctly
-- declined to invent one. This is the column that gives it something to say.
--
-- NOT NULL DEFAULT '' rather than nullable, matching image_path directly above
-- it: "no image" and "an image nobody described" both render as alt="", which
-- is the right treatment for an image whose meaning is not stated, so a reader
-- has no use for the difference. A nullable column would make every read path
-- spell out both forms of empty to arrive at the same markup.
--
-- Bounded at 500 characters in the service rather than here, like every other
-- free-text field on this schema. The bound is a UI and screen-reader-listening
-- judgement — an alt string is read aloud in one breath — and moving it into the
-- column type would make changing one an ALTER TABLE.
--
-- Only meaningful on a post that has an image, and the service refuses to store
-- it otherwise. That rule is not expressible as a CHECK worth having: image_path
-- can be cleared by no code path that exists today, so a constraint would be
-- guarding against a write nobody makes while making the imageless case fail
-- with a Postgres error instead of a 400 that says what went wrong.
ALTER TABLE posts ADD COLUMN alt_text TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE posts DROP COLUMN IF EXISTS alt_text;

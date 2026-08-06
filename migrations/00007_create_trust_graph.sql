-- +goose Up
-- Create the trust graph in Apache AGE
-- Note: AGE queries require loading the extension into the search path
LOAD 'age';
SET search_path = ag_catalog, "$user", public;

SELECT create_graph('trust_graph');

-- Reset search_path so subsequent migrations create tables in public schema
SET search_path = public, ag_catalog, "$user";

-- +goose Down
LOAD 'age';
SET search_path = ag_catalog, "$user", public;

SELECT drop_graph('trust_graph', true);

-- Reset search_path, for the same reason the Up block above does it.
--
-- `SET search_path` is session state and outlives the migration that set it, so
-- without this the connection is left resolving into ag_catalog for everything
-- that follows. Down blocks run in reverse order, which puts 00006 through
-- 00002 after this one, and leaves whatever the caller does on that connection
-- after the rollback exposed too. Verified before this line existed: a plain
-- CREATE TABLE issued after rolling past 00007 landed in ag_catalog.
--
-- The full round trip happened to survive only because this file's Up block
-- re-runs on the way back and resets the path before any table is created.
-- That is luck, not a guarantee.
SET search_path = public, ag_catalog, "$user";

-- +goose Up
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS age;

-- Load AGE into the search path for this session, then reset to public-first
-- so subsequent DDL (tables, indexes) lands in the public schema.
SET search_path = ag_catalog, "$user", public;
SET search_path = public, ag_catalog, "$user";

-- +goose Down
DROP EXTENSION IF EXISTS age CASCADE;
DROP EXTENSION IF EXISTS "uuid-ossp";

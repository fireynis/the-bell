-- +goose Up
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS age;

-- Load AGE into the search path
SET search_path = ag_catalog, "$user", public;

-- +goose Down
DROP EXTENSION IF EXISTS age CASCADE;
DROP EXTENSION IF EXISTS "uuid-ossp";

-- +goose Up
-- Create the trust graph in Apache AGE
-- Note: AGE queries require loading the extension into the search path
LOAD 'age';
SET search_path = ag_catalog, "$user", public;

SELECT create_graph('trust_graph');

-- +goose Down
LOAD 'age';
SET search_path = ag_catalog, "$user", public;

SELECT drop_graph('trust_graph', true);

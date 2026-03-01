-- name: GetTownConfig :one
SELECT value FROM town_config WHERE key = $1;

-- name: SetTownConfig :exec
INSERT INTO town_config (key, value) VALUES ($1, $2)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

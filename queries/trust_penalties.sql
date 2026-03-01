-- name: CreateTrustPenalty :one
INSERT INTO trust_penalties (id, user_id, moderation_action_id, penalty_amount, hop_depth, created_at, decays_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

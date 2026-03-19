package cache

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/redis/go-redis/v9"
)

const (
	workerPollTimeout = 5 * time.Second
	baseTrustScore    = 100.0
)

// PenaltyQuerier retrieves active penalties for a user.
type PenaltyQuerier interface {
	ListActivePenaltiesByUser(ctx context.Context, userID string) ([]domain.TrustPenalty, error)
}

// TrustScoreUpdater persists a recalculated trust score to the database.
type TrustScoreUpdater interface {
	UpdateUserTrustScore(ctx context.Context, id string, score float64) error
}

// TrustWorker polls the Redis recalculation queue and updates trust scores.
type TrustWorker struct {
	cache     *TrustCache
	penalties PenaltyQuerier
	users     TrustScoreUpdater
	logger    *slog.Logger
}

// NewTrustWorker creates a TrustWorker.
func NewTrustWorker(cache *TrustCache, penalties PenaltyQuerier, users TrustScoreUpdater, logger *slog.Logger) *TrustWorker {
	return &TrustWorker{
		cache:     cache,
		penalties: penalties,
		users:     users,
		logger:    logger,
	}
}

// Run blocks, polling the recalculation queue until ctx is cancelled.
func (w *TrustWorker) Run(ctx context.Context) {
	w.logger.Info("trust worker started")
	for {
		select {
		case <-ctx.Done():
			w.logger.Info("trust worker stopping")
			return
		default:
		}

		userID, err := w.cache.DequeueRecalc(ctx, workerPollTimeout)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				continue
			}
			// BLPop returns redis.Nil on timeout; ignore it.
			if errors.Is(err, redis.Nil) {
				continue
			}
			w.logger.Warn("trust worker dequeue error", "error", err)
			continue
		}

		if err := w.recalculate(ctx, userID); err != nil {
			w.logger.Error("trust recalculation failed", "user_id", userID, "error", err)
		}
	}
}

// recalculate computes the trust score for a user and updates both cache and DB.
// For now: base score (100) minus sum of active penalty amounts.
func (w *TrustWorker) recalculate(ctx context.Context, userID string) error {
	penalties, err := w.penalties.ListActivePenaltiesByUser(ctx, userID)
	if err != nil {
		return err
	}

	var totalPenalty float64
	for _, p := range penalties {
		totalPenalty += p.PenaltyAmount
	}

	score := baseTrustScore - totalPenalty
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	if err := w.users.UpdateUserTrustScore(ctx, userID, score); err != nil {
		return err
	}

	if err := w.cache.SetTrustScore(ctx, userID, score); err != nil {
		w.logger.Warn("trust cache set failed after recalc", "user_id", userID, "error", err)
		// Non-fatal: DB is the source of truth.
	}

	w.logger.Debug("trust score recalculated", "user_id", userID, "score", score, "penalties", len(penalties))
	return nil
}

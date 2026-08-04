package cache

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/fireynis/the-bell/internal/service"
	"github.com/redis/go-redis/v9"
)

const workerPollTimeout = 5 * time.Second

// TrustScoreUpdater persists a recalculated trust score to the database.
type TrustScoreUpdater interface {
	UpdateUserTrustScore(ctx context.Context, id string, score float64) error
}

// TrustWorker polls the Redis recalculation queue and updates trust scores.
type TrustWorker struct {
	cache  *TrustCache
	inputs service.TrustInputs
	users  TrustScoreUpdater
	logger *slog.Logger
	now    func() time.Time
	// pollTimeout is how long each blocking dequeue waits before looping.
	// Tests shorten it so the idle path can be exercised without a real wait.
	pollTimeout time.Duration
}

// NewTrustWorker creates a TrustWorker.
func NewTrustWorker(cache *TrustCache, inputs service.TrustInputs, users TrustScoreUpdater, logger *slog.Logger) *TrustWorker {
	return &TrustWorker{
		cache:       cache,
		inputs:      inputs,
		users:       users,
		logger:      logger,
		now:         time.Now,
		pollTimeout: workerPollTimeout,
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

		userID, err := w.cache.DequeueRecalc(ctx, w.pollTimeout)
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
// The score itself is the four-component composite; this method only moves the
// result into storage.
func (w *TrustWorker) recalculate(ctx context.Context, userID string) error {
	score, err := service.CalcCompositeTrust(ctx, w.inputs, userID, w.now())
	if err != nil {
		return err
	}

	if err := w.users.UpdateUserTrustScore(ctx, userID, score); err != nil {
		return err
	}

	if err := w.cache.SetTrustScore(ctx, userID, score); err != nil {
		w.logger.Warn("trust cache set failed after recalc", "user_id", userID, "error", err)
		// Non-fatal: DB is the source of truth.
	}

	w.logger.Debug("trust score recalculated", "user_id", userID, "score", score)
	return nil
}

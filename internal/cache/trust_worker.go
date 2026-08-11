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

// pollOutcome is what one dequeue attempt means for the loop.
type pollOutcome int

const (
	// pollDispatch: a user ID came back and should be recalculated.
	pollDispatch pollOutcome = iota
	// pollIdle: nothing to do. The loop polls again without complaining.
	pollIdle
	// pollFailed: the dequeue itself went wrong and is worth logging.
	pollFailed
)

// classifyPoll interprets the error from one DequeueRecalc call.
//
// Three of the errors it can return are not failures. BLPOP reports an empty
// queue as redis.Nil, and a cancelled or expired context is either the worker
// being asked to stop or a poll simply reaching its timeout. A town where
// nothing is happening is the normal state, so logging any of these would emit
// a line every pollTimeout forever and bury the errors that matter.
func classifyPoll(err error) pollOutcome {
	switch {
	case err == nil:
		return pollDispatch
	case errors.Is(err, redis.Nil),
		errors.Is(err, context.Canceled),
		errors.Is(err, context.DeadlineExceeded):
		return pollIdle
	default:
		return pollFailed
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
		switch classifyPoll(err) {
		case pollIdle:
			continue
		case pollFailed:
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

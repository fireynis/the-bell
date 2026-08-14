package cache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/redis/go-redis/v9"
)

const workerPollTimeout = 5 * time.Second

// defaultSweepInterval is how often every active user is put back through the
// trust calculation. Daily matches the nightly batch the design called for, and
// suits inputs that move with the calendar: the shortest penalty decay window
// is measured in days, and tenure resolves to a day.
const defaultSweepInterval = 24 * time.Hour

// TrustScoreUpdater persists a recalculated trust score to the database.
type TrustScoreUpdater interface {
	UpdateUserTrustScore(ctx context.Context, id string, score float64) error
}

// TrustUserLister supplies the roster a periodic sweep walks. It is the same
// listing the role checker evaluates, which is the right population twice over:
// a banned user's score is pinned by the calculation anyway, and an inactive
// one has nothing to recompute.
type TrustUserLister interface {
	ListActiveNonBannedUsers(ctx context.Context) ([]*domain.User, error)
}

// TrustWorker polls the Redis recalculation queue and updates trust scores.
type TrustWorker struct {
	cache  *TrustCache
	inputs service.TrustInputs
	users  TrustScoreUpdater
	roster TrustUserLister
	logger *slog.Logger
	now    func() time.Time
	// pollTimeout is how long each blocking dequeue waits before looping.
	// Tests shorten it so the idle path can be exercised without a real wait.
	pollTimeout time.Duration
	// sweepInterval is how often the full sweep runs. See SetSweepInterval.
	sweepInterval time.Duration
}

// NewTrustWorker creates a TrustWorker.
//
// roster may be nil, which leaves the periodic sweep off; the worker still
// drains the queue. Production always supplies one — a worker that only reacts
// to events lets scores drift for everyone nothing is happening to.
func NewTrustWorker(cache *TrustCache, inputs service.TrustInputs, users TrustScoreUpdater, roster TrustUserLister, logger *slog.Logger) *TrustWorker {
	return &TrustWorker{
		cache:         cache,
		inputs:        inputs,
		users:         users,
		roster:        roster,
		logger:        logger,
		now:           time.Now,
		pollTimeout:   workerPollTimeout,
		sweepInterval: defaultSweepInterval,
	}
}

// SetSweepInterval overrides how often the full sweep runs. A non-positive
// interval is ignored, so a misconfigured value cannot turn the loop into a
// spin.
func (w *TrustWorker) SetSweepInterval(d time.Duration) {
	if d <= 0 {
		w.logger.Warn("ignoring non-positive trust sweep interval", "interval", d)
		return
	}
	w.sweepInterval = d
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

// Run blocks, polling the recalculation queue until ctx is cancelled. The
// periodic sweep runs alongside it and stops with it.
func (w *TrustWorker) Run(ctx context.Context) {
	w.logger.Info("trust worker started", "sweep_interval", w.sweepInterval)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.runSweeps(ctx)
	}()
	// The dequeue below returns on cancellation, and the sweep loop watches the
	// same context, so waiting here means Run outlives both rather than leaving
	// a goroutine enqueueing into a shutting-down process.
	defer wg.Wait()

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

// runSweeps sweeps once at startup and then on every tick until ctx is
// cancelled.
//
// The startup sweep is what repairs a town that has been running without one:
// scores that have gone stale — or, on a deployment that has never had Redis at
// all, never been computed since the 50.0 default the row was created with —
// are corrected within a poll of the process coming up rather than a day later.
// A restarting process re-sweeping is cheap: the work is one enqueue per active
// user, and a duplicate costs a single recalculation.
func (w *TrustWorker) runSweeps(ctx context.Context) {
	if w.roster == nil {
		w.logger.Warn("trust worker has no user roster: scores will only be recalculated " +
			"when something happens to a user, so penalties never decay and tenure never accrues")
		return
	}

	ticker := time.NewTicker(w.sweepInterval)
	defer ticker.Stop()

	for {
		enqueued, err := w.sweep(ctx)
		if err != nil {
			// Logged and retried on the next tick rather than ending the loop:
			// a database blip must not leave the process permanently without a
			// sweep until someone restarts it.
			w.logger.Error("trust sweep failed", "error", err)
		} else {
			w.logger.Info("trust sweep enqueued users", "count", enqueued)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// sweep enqueues every active user for recalculation, returning how many it
// managed to enqueue.
//
// It goes through the queue rather than recalculating inline so the drain loop
// stays the single path that computes and writes a score, and so a town-sized
// sweep arrives as a stream of jobs instead of one long transaction. One user
// who cannot be enqueued does not abandon the rest — the sweep is the only
// thing that will revisit them, so it is worth finishing.
func (w *TrustWorker) sweep(ctx context.Context) (int, error) {
	if w.roster == nil {
		return 0, nil
	}

	users, err := w.roster.ListActiveNonBannedUsers(ctx)
	if err != nil {
		return 0, fmt.Errorf("listing users for trust sweep: %w", err)
	}

	var enqueued int
	for _, u := range users {
		if err := w.cache.EnqueueRecalc(ctx, u.ID); err != nil {
			w.logger.Warn("trust sweep enqueue failed", "user_id", u.ID, "error", err)
			continue
		}
		enqueued++
	}
	return enqueued, nil
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

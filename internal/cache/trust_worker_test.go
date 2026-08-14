package cache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/redis/go-redis/v9"
)

// fakeTrustInputs is an in-process service.TrustInputs. The score arithmetic
// itself is covered by TestCalcCompositeTrust in the service package; here it
// only needs to supply enough for the worker to produce a known number.
type fakeTrustInputs struct {
	user      *domain.User
	posts     int64
	reactions int64
	vouches   int64
	avgTrust  float64
	penalties []domain.TrustPenalty
	err       error
	// errForUser fails the lookup for specific users only. It is set once
	// before the worker starts: a loop test must not mutate this fake while
	// the worker goroutine is reading it.
	errForUser map[string]error
	// called signals each lookup, so a loop test can tell that the worker
	// reached the calculation even when it goes on to fail.
	called chan struct{}
}

func (f *fakeTrustInputs) GetUserByID(_ context.Context, id string) (*domain.User, error) {
	if f.called != nil {
		select {
		case f.called <- struct{}{}:
		default:
		}
	}
	if err, ok := f.errForUser[id]; ok {
		return nil, err
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.user != nil {
		return f.user, nil
	}
	return &domain.User{ID: id}, nil
}

func (f *fakeTrustInputs) CountPostsByAuthorSince(_ context.Context, _ string, _ time.Time) (int64, error) {
	return f.posts, nil
}

func (f *fakeTrustInputs) CountReactionsReceivedByAuthorSince(_ context.Context, _ string, _ time.Time) (int64, error) {
	return f.reactions, nil
}

func (f *fakeTrustInputs) CountActiveVouchesWithAvgTrust(_ context.Context, _ string) (int64, float64, error) {
	return f.vouches, f.avgTrust, nil
}

func (f *fakeTrustInputs) ListActivePenaltiesByUser(_ context.Context, _ string) ([]domain.TrustPenalty, error) {
	return f.penalties, nil
}

// stubTrustScoreUpdater records calls and signals each one, so a test driving
// the worker loop can stop as soon as the work lands instead of waiting out a
// timeout.
type stubTrustScoreUpdater struct {
	mu    sync.Mutex
	calls []trustUpdate
	seen  chan trustUpdate
	err   error
}

type trustUpdate struct {
	id    string
	score float64
}

func newStubTrustScoreUpdater() *stubTrustScoreUpdater {
	return &stubTrustScoreUpdater{seen: make(chan trustUpdate, 8)}
}

func (s *stubTrustScoreUpdater) UpdateUserTrustScore(_ context.Context, id string, score float64) error {
	if s.err != nil {
		return s.err
	}
	s.mu.Lock()
	s.calls = append(s.calls, trustUpdate{id, score})
	s.mu.Unlock()
	select {
	case s.seen <- trustUpdate{id, score}:
	default:
	}
	return nil
}

func (s *stubTrustScoreUpdater) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// stubUserLister is the roster a sweep walks.
type stubUserLister struct {
	users []*domain.User
	err   error
}

func (s *stubUserLister) ListActiveNonBannedUsers(_ context.Context) ([]*domain.User, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.users, nil
}

var workerNow = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

// newTestWorker builds a worker with a pinned clock so the composite score is
// deterministic.
func newTestWorker(t *testing.T, inputs *fakeTrustInputs, updater *stubTrustScoreUpdater) (*TrustWorker, *TrustCache) {
	t.Helper()
	tc := NewTrustCache(redisAvailable(t))
	w := NewTrustWorker(tc, inputs, updater, nil, testLogger())
	w.now = func() time.Time { return workerNow }
	// One second is the shortest poll Redis honours — BLPOP takes its timeout
	// in seconds and go-redis rounds anything smaller up — and it keeps the
	// loop tests off the production five-second wait.
	w.pollTimeout = time.Second
	return w, tc
}

// The worker must write the four-component composite, not the old
// "100 minus penalties" placeholder. A user with a year of tenure and no
// activity, vouches or penalties scores 15 (tenure) + 30 (moderation) = 45,
// where the placeholder would have written 100.
func TestTrustWorker_Recalculate_WritesCompositeScore(t *testing.T) {
	inputs := &fakeTrustInputs{
		user: &domain.User{ID: "user-recalc", JoinedAt: workerNow.AddDate(0, 0, -365)},
	}
	updater := newStubTrustScoreUpdater()
	w, tc := newTestWorker(t, inputs, updater)

	if err := w.recalculate(context.Background(), "user-recalc"); err != nil {
		t.Fatalf("recalculate() unexpected error: %v", err)
	}

	if updater.callCount() != 1 {
		t.Fatalf("expected 1 update call, got %d", updater.callCount())
	}
	got := updater.calls[0]
	if got.id != "user-recalc" {
		t.Errorf("updated user = %q, want %q", got.id, "user-recalc")
	}
	const want = 45.0
	if got.score != want {
		t.Errorf("score = %v, want %v", got.score, want)
	}

	// The cache is written through so readers do not have to wait for a miss.
	cached, ok := tc.GetTrustScore(context.Background(), "user-recalc")
	if !ok {
		t.Fatal("expected a cache hit after recalculation")
	}
	if cached != want {
		t.Errorf("cached score = %v, want %v", cached, want)
	}
}

// A score that cannot be computed must not be written: persisting a partial
// calculation would silently corrupt the user's standing.
func TestTrustWorker_Recalculate_CalculationFailureWritesNothing(t *testing.T) {
	inputs := &fakeTrustInputs{err: errors.New("db connection lost")}
	updater := newStubTrustScoreUpdater()
	w, tc := newTestWorker(t, inputs, updater)

	if err := w.recalculate(context.Background(), "user-fail"); err == nil {
		t.Fatal("recalculate() expected an error, got nil")
	}
	if updater.callCount() != 0 {
		t.Errorf("update calls = %d, want 0", updater.callCount())
	}
	if _, ok := tc.GetTrustScore(context.Background(), "user-fail"); ok {
		t.Error("a score was cached despite the calculation failing")
	}
}

// The database is the source of truth, so a persist failure aborts the job
// rather than leaving a cached score the database does not have.
func TestTrustWorker_Recalculate_PersistFailureIsNotCached(t *testing.T) {
	inputs := &fakeTrustInputs{user: &domain.User{ID: "user-nopersist", JoinedAt: workerNow}}
	updater := newStubTrustScoreUpdater()
	updater.err = errors.New("db write failed")
	w, tc := newTestWorker(t, inputs, updater)

	if err := w.recalculate(context.Background(), "user-nopersist"); err == nil {
		t.Fatal("recalculate() expected an error, got nil")
	}
	if _, ok := tc.GetTrustScore(context.Background(), "user-nopersist"); ok {
		t.Error("a score was cached even though the database write failed")
	}
}

// The one loop test: every case above calls recalculate directly, so this only
// has to prove that a queued user reaches it. It doubles as the resilience
// case, because a queue shared by the whole town cannot be stopped by one
// user whose score will not compute — the failing job is enqueued first and the
// healthy one behind it still lands.
//
// It is driven entirely by channel signals rather than sleeps: the worker's
// poll interval is Redis's one-second floor, and waiting that out on every run
// is what made the earlier version of this file cost seconds.
func TestTrustWorker_Run_RecalculatesQueuedUsersDespiteAFailedOne(t *testing.T) {
	// Both outcomes are configured up front. Flipping the fake's fields once
	// the worker is running would race it, and the worker could pick up the
	// relaxed setting while still handling the job meant to fail.
	inputs := &fakeTrustInputs{
		user:       &domain.User{ID: "user-ok", JoinedAt: workerNow.AddDate(0, 0, -365)},
		errForUser: map[string]error{"user-broken": errors.New("db connection lost")},
		called:     make(chan struct{}, 4),
	}
	updater := newStubTrustScoreUpdater()
	w, tc := newTestWorker(t, inputs, updater)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	// FIFO: the failing job is consumed first, then the healthy one.
	for _, userID := range []string{"user-broken", "user-ok"} {
		if err := tc.EnqueueRecalc(ctx, userID); err != nil {
			t.Fatalf("enqueue %s: %v", userID, err)
		}
	}

	select {
	case <-inputs.called:
	case <-time.After(10 * time.Second):
		t.Fatal("worker never attempted the recalculation")
	}

	// The only score that may ever be written is the healthy user's, and it is
	// the full composite the worker computed, not a placeholder.
	select {
	case got := <-updater.seen:
		if got.id != "user-ok" {
			t.Errorf("updated user = %q, want only %q — the failing job must write nothing", got.id, "user-ok")
		}
		if got.score != 45.0 {
			t.Errorf("score = %v, want 45", got.score)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("worker stopped consuming after a failed recalculation")
	}

	if updater.callCount() != 1 {
		t.Errorf("update calls = %d, want exactly 1 (the healthy user)", updater.callCount())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not stop after its context was cancelled")
	}
}

// --- The periodic sweep ---

// The queue only ever carries users something just happened to. Tenure accrues,
// activity ages out of its 90-day window and penalties decay with the calendar
// alone, so a user nobody interacts with keeps whatever score they last had
// forever — a penalty that expired months ago still costing them, a member who
// has passed a year of tenure never collecting it. The sweep is what puts every
// active user back through the calculation on a schedule.
func TestTrustWorker_Sweep_EnqueuesEveryActiveUser(t *testing.T) {
	inputs := &fakeTrustInputs{}
	w, tc := newTestWorker(t, inputs, newStubTrustScoreUpdater())
	w.roster = &stubUserLister{users: []*domain.User{
		{ID: "user-1"}, {ID: "user-2"}, {ID: "user-3"},
	}}

	ctx := context.Background()
	enqueued, err := w.sweep(ctx)
	if err != nil {
		t.Fatalf("sweep() unexpected error: %v", err)
	}
	if enqueued != 3 {
		t.Errorf("enqueued = %d, want 3", enqueued)
	}

	got := map[string]bool{}
	for range 3 {
		userID, err := tc.DequeueRecalc(ctx, time.Second)
		if err != nil {
			t.Fatalf("dequeue: %v", err)
		}
		got[userID] = true
	}
	for _, want := range []string{"user-1", "user-2", "user-3"} {
		if !got[want] {
			t.Errorf("user %q was not enqueued by the sweep", want)
		}
	}
}

// A roster that cannot be read is a failed sweep, not an empty town: reporting
// it as success would hide a broken sweep behind a quiet log.
func TestTrustWorker_Sweep_ListFailureIsReported(t *testing.T) {
	w, _ := newTestWorker(t, &fakeTrustInputs{}, newStubTrustScoreUpdater())
	w.roster = &stubUserLister{err: errors.New("db connection lost")}

	if _, err := w.sweep(context.Background()); err == nil {
		t.Fatal("sweep() expected an error, got nil")
	}
}

// The worker must survive being built without a roster rather than panicking
// on the first tick.
func TestTrustWorker_Sweep_WithoutARosterIsANoOp(t *testing.T) {
	w, _ := newTestWorker(t, &fakeTrustInputs{}, newStubTrustScoreUpdater())

	enqueued, err := w.sweep(context.Background())
	if err != nil {
		t.Fatalf("sweep() unexpected error: %v", err)
	}
	if enqueued != 0 {
		t.Errorf("enqueued = %d, want 0", enqueued)
	}
}

// time.NewTicker panics on a non-positive interval, and a zero value is exactly
// what an unset configuration knob hands over. Keeping the default is a far
// better answer than taking the worker down with it.
func TestTrustWorker_SetSweepInterval_IgnoresNonPositive(t *testing.T) {
	w, _ := newTestWorker(t, &fakeTrustInputs{}, newStubTrustScoreUpdater())

	for _, d := range []time.Duration{0, -time.Minute} {
		w.SetSweepInterval(d)
		if w.sweepInterval != defaultSweepInterval {
			t.Errorf("SetSweepInterval(%v): interval = %v, want the default %v",
				d, w.sweepInterval, defaultSweepInterval)
		}
	}

	w.SetSweepInterval(time.Hour)
	if w.sweepInterval != time.Hour {
		t.Errorf("interval = %v, want 1h", w.sweepInterval)
	}
}

// End to end through the real queue: Run sweeps at startup, and its own drain
// loop picks the swept user up and writes the composite. Nothing enqueues this
// user — the point is that the worker does it to itself.
func TestTrustWorker_Run_SweepsAtStartup(t *testing.T) {
	inputs := &fakeTrustInputs{
		user: &domain.User{ID: "user-idle", JoinedAt: workerNow.AddDate(0, 0, -365)},
	}
	updater := newStubTrustScoreUpdater()
	w, _ := newTestWorker(t, inputs, updater)
	w.roster = &stubUserLister{users: []*domain.User{{ID: "user-idle"}}}
	// Long enough that only the startup sweep can be responsible for what
	// lands below.
	w.SetSweepInterval(time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	select {
	case got := <-updater.seen:
		if got.id != "user-idle" {
			t.Errorf("updated user = %q, want %q", got.id, "user-idle")
		}
		if got.score != 45.0 {
			t.Errorf("score = %v, want 45", got.score)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the startup sweep never reached a user nobody had enqueued")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not stop after its context was cancelled")
	}
}

// Production hands the services this same TrustCache as their recalculation
// queue, so the two halves of the pipeline have to agree on the interface.
// Without a producer calling EnqueueRecalc the worker blocks on an empty list
// forever, which is exactly the state this work is fixing.
var _ service.TrustRecalcQueue = (*TrustCache)(nil)

// --- classifyPoll ---

// Three of the errors a dequeue can return are not failures: BLPOP reports an
// empty queue as redis.Nil, and a cancelled or expired context is how the
// worker is asked to stop or how a poll simply times out. Classifying any of
// them as a failure would log a line every poll interval for a town where
// nothing is happening, which is the normal state.
func TestClassifyPoll(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want pollOutcome
	}{
		{"a dequeued user is dispatched", nil, pollDispatch},
		{"an empty queue is idle, not an error", redis.Nil, pollIdle},
		{"a cancelled context is idle", context.Canceled, pollIdle},
		{"an expired poll is idle", context.DeadlineExceeded, pollIdle},
		{"a wrapped redis.Nil is still idle", fmt.Errorf("dequeue recalc: %w", redis.Nil), pollIdle},
		{"a wrapped cancellation is still idle", fmt.Errorf("dequeue recalc: %w", context.Canceled), pollIdle},
		{"an unreachable Redis is a real failure", errors.New("dial tcp: connection refused"), pollFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyPoll(tt.err); got != tt.want {
				t.Errorf("classifyPoll(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

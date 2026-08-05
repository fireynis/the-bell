package cache

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/service"
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

var workerNow = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

// newTestWorker builds a worker with a pinned clock so the composite score is
// deterministic.
func newTestWorker(t *testing.T, inputs *fakeTrustInputs, updater *stubTrustScoreUpdater) (*TrustWorker, *TrustCache) {
	t.Helper()
	tc := NewTrustCache(redisAvailable(t))
	w := NewTrustWorker(tc, inputs, updater, testLogger())
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

// Penalties now decay rather than subtracting forever. A severity-3 penalty
// (25 points over 270 days) that is half elapsed costs 12.5, so moderation is
// 87.5 and contributes 26.25 on top of the 15 from a full year of tenure.
func TestTrustWorker_Recalculate_PenaltiesDecay(t *testing.T) {
	created := workerNow.AddDate(0, 0, -135)
	decaysAt := created.AddDate(0, 0, 270)
	inputs := &fakeTrustInputs{
		user: &domain.User{ID: "user-decay", JoinedAt: workerNow.AddDate(0, 0, -365)},
		penalties: []domain.TrustPenalty{
			{PenaltyAmount: 25, CreatedAt: created, DecaysAt: &decaysAt},
		},
	}
	updater := newStubTrustScoreUpdater()
	w, _ := newTestWorker(t, inputs, updater)

	if err := w.recalculate(context.Background(), "user-decay"); err != nil {
		t.Fatalf("recalculate() unexpected error: %v", err)
	}

	const want = 41.25
	if got := updater.calls[0].score; got != want {
		t.Errorf("score = %v, want %v", got, want)
	}
}

// A permanent penalty (nil DecaysAt) never decays, so a banned user's
// moderation component stays at zero however long ago the ban was.
func TestTrustWorker_Recalculate_PermanentPenaltyDoesNotDecay(t *testing.T) {
	inputs := &fakeTrustInputs{
		user: &domain.User{ID: "user-banned", JoinedAt: workerNow.AddDate(-2, 0, 0)},
		penalties: []domain.TrustPenalty{
			{PenaltyAmount: 100, CreatedAt: workerNow.AddDate(-1, 0, 0)},
		},
	}
	updater := newStubTrustScoreUpdater()
	w, _ := newTestWorker(t, inputs, updater)

	if err := w.recalculate(context.Background(), "user-banned"); err != nil {
		t.Fatalf("recalculate() unexpected error: %v", err)
	}

	// Only tenure survives: 100 * 0.15 = 15.
	const want = 15.0
	if got := updater.calls[0].score; got != want {
		t.Errorf("score = %v, want %v", got, want)
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

// The loop test: everything above calls recalculate directly, so this only has
// to prove that a queued user reaches it. It stops as soon as the work lands
// rather than waiting out a fixed timeout.
func TestTrustWorker_Run_ProcessesQueuedUser(t *testing.T) {
	inputs := &fakeTrustInputs{
		user: &domain.User{ID: "user-queued", JoinedAt: workerNow.AddDate(0, 0, -365)},
	}
	updater := newStubTrustScoreUpdater()
	w, tc := newTestWorker(t, inputs, updater)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := tc.EnqueueRecalc(ctx, "user-queued"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	select {
	case got := <-updater.seen:
		if got.id != "user-queued" {
			t.Errorf("updated user = %q, want %q", got.id, "user-queued")
		}
		if got.score != 45.0 {
			t.Errorf("score = %v, want 45", got.score)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not process the queued user")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not stop after its context was cancelled")
	}
}

// An empty queue is the normal steady state, not an error: BLPop returns
// redis.Nil on timeout and the worker must keep polling. This exercises the
// idle path, then confirms a job arriving afterwards is still picked up.
func TestTrustWorker_Run_KeepsPollingWhileIdle(t *testing.T) {
	inputs := &fakeTrustInputs{
		user: &domain.User{ID: "user-late", JoinedAt: workerNow.AddDate(0, 0, -365)},
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

	// Let a poll timeout elapse against an empty queue.
	time.Sleep(1300 * time.Millisecond)
	if updater.callCount() != 0 {
		t.Fatalf("update calls = %d, want 0 while the queue is empty", updater.callCount())
	}

	if err := tc.EnqueueRecalc(ctx, "user-late"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	select {
	case got := <-updater.seen:
		if got.id != "user-late" {
			t.Errorf("updated user = %q, want %q", got.id, "user-late")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not pick up a job enqueued after idling")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not stop after its context was cancelled")
	}
}

// One user whose score cannot be computed must not take the worker down: the
// queue is shared by the whole town, so the loop logs and carries on.
func TestTrustWorker_Run_SurvivesARecalculationFailure(t *testing.T) {
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

	// The only score that may ever be written is the healthy user's.
	select {
	case got := <-updater.seen:
		if got.id != "user-ok" {
			t.Errorf("updated user = %q, want only %q — the failing job must write nothing", got.id, "user-ok")
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

// Production hands the services this same TrustCache as their recalculation
// queue, so the two halves of the pipeline have to agree on the interface.
// Without a producer calling EnqueueRecalc the worker blocks on an empty list
// forever, which is exactly the state this work is fixing.
var _ service.TrustRecalcQueue = (*TrustCache)(nil)

package cache

import (
	"context"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// stubPenaltyQuerier returns a fixed list of penalties.
type stubPenaltyQuerier struct {
	penalties []domain.TrustPenalty
	err       error
}

func (s *stubPenaltyQuerier) ListActivePenaltiesByUser(_ context.Context, _ string) ([]domain.TrustPenalty, error) {
	return s.penalties, s.err
}

// stubTrustScoreUpdater records calls.
type stubTrustScoreUpdater struct {
	calls []struct {
		id    string
		score float64
	}
}

func (s *stubTrustScoreUpdater) UpdateUserTrustScore(_ context.Context, id string, score float64) error {
	s.calls = append(s.calls, struct {
		id    string
		score float64
	}{id, score})
	return nil
}

func TestTrustWorker_Recalculate(t *testing.T) {
	rdb := redisAvailable(t)
	tc := NewTrustCache(rdb)

	penalties := &stubPenaltyQuerier{
		penalties: []domain.TrustPenalty{
			{PenaltyAmount: 10.0},
			{PenaltyAmount: 5.0},
		},
	}
	updater := &stubTrustScoreUpdater{}

	w := NewTrustWorker(tc, penalties, updater, testLogger())
	ctx := context.Background()

	// Enqueue a recalc job
	if err := tc.EnqueueRecalc(ctx, "user-recalc"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Run worker briefly
	workerCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		w.Run(workerCtx)
		close(done)
	}()

	<-done

	// Check DB was updated
	if len(updater.calls) != 1 {
		t.Fatalf("expected 1 update call, got %d", len(updater.calls))
	}
	if updater.calls[0].id != "user-recalc" {
		t.Fatalf("expected user-recalc, got %s", updater.calls[0].id)
	}
	expected := 85.0 // 100 - 10 - 5
	if updater.calls[0].score != expected {
		t.Fatalf("expected score %f, got %f", expected, updater.calls[0].score)
	}

	// Check cache was updated
	score, ok := tc.GetTrustScore(ctx, "user-recalc")
	if !ok {
		t.Fatal("expected cache hit after recalc")
	}
	if score != expected {
		t.Fatalf("expected cached score %f, got %f", expected, score)
	}
}

func TestTrustWorker_ScoreFloor(t *testing.T) {
	rdb := redisAvailable(t)
	tc := NewTrustCache(rdb)

	penalties := &stubPenaltyQuerier{
		penalties: []domain.TrustPenalty{
			{PenaltyAmount: 60.0},
			{PenaltyAmount: 60.0},
		},
	}
	updater := &stubTrustScoreUpdater{}

	w := NewTrustWorker(tc, penalties, updater, testLogger())

	if err := tc.EnqueueRecalc(context.Background(), "user-floor"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	workerCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		w.Run(workerCtx)
		close(done)
	}()

	<-done

	if len(updater.calls) != 1 {
		t.Fatalf("expected 1 update call, got %d", len(updater.calls))
	}
	if updater.calls[0].score != 0 {
		t.Fatalf("expected score 0 (floor), got %f", updater.calls[0].score)
	}
}

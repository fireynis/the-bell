//go:build integration

package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fireynis/the-bell/internal/config"
	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/server"
	"github.com/fireynis/the-bell/internal/testsupport"
)

// TestMain delegates to testsupport so this package shares one Postgres and one
// Redis container rather than starting a pair per test.
func TestMain(m *testing.M) {
	os.Exit(testsupport.RunMain(m))
}

// These tests exist because the trust engine was once complete and inert. Every
// piece was correct in isolation — the composite calculator, the worker, the
// enqueue calls with their nil guards — but Build never handed the services a
// queue, so trustQueue was nil, every enqueue returned at its guard, the worker
// blocked on an empty list forever, and no score was ever recomputed in
// production. Nothing was broken enough to fail a unit test: the services were
// constructed correctly and the guard is legitimate behaviour when Redis is
// absent.
//
// So these assert on the observable end of the pipeline instead. They drive a
// real moderation action and a real vouch through the graph Build returns,
// against real Postgres and Redis, run the real worker, and require the user's
// stored trust score to actually move. A missing SetTrustQueue makes them hang
// until their deadline and fail, which is what should have happened the first
// time.

// buildTestDeps wires the production graph over real containers.
func buildTestDeps(t *testing.T, pool *pgxpool.Pool, withRedis bool) *Deps {
	t.Helper()

	cfg := config.Config{
		Port:        8080,
		DatabaseURL: "unused",
		// Any absolute URL will do: no test here resolves this host.
		KratosPublicURL:  "http://kratos.invalid",
		KratosAdminURL:   "http://kratos.invalid",
		ImageStoragePath: t.TempDir(),
	}

	var deps *Deps
	var err error
	if withRedis {
		deps, err = Build(cfg, pool, testsupport.TestRedis(t), testsupport.DiscardLogger())
	} else {
		deps, err = Build(cfg, pool, nil, testsupport.DiscardLogger())
	}
	if err != nil {
		t.Fatalf("building app dependencies: %v", err)
	}
	return deps
}

// startTrustWorker runs the worker Build produced and stops it when the test ends.
func startTrustWorker(t *testing.T, deps *Deps) context.Context {
	t.Helper()

	if deps.TrustWorker == nil {
		t.Fatal("Build returned no TrustWorker despite being given a Redis client")
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		deps.TrustWorker.Run(ctx)
		close(stopped)
	}()

	t.Cleanup(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(15 * time.Second):
			t.Error("trust worker did not stop after its context was cancelled")
		}
	})

	return ctx
}

// awaitTrustChange polls until the user's stored trust score moves off the
// value it was seeded with. The whole point of the pipeline is that this
// happens without anyone asking for it, so a timeout here means the recalc
// never fired.
func awaitTrustChange(t *testing.T, deps *Deps, userID string, seeded float64) float64 {
	t.Helper()

	ctx := context.Background()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		user, err := deps.UserService.GetByID(ctx, userID)
		if err != nil {
			t.Fatalf("reading back user %s: %v", userID, err)
		}
		if user.TrustScore != seeded {
			return user.TrustScore
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("trust score for %s never moved off its seeded %v — "+
		"the recalculation queue is not wired to the services", userID, seeded)
	return 0
}

// A warn carries a trust penalty but no enforcement, so the only thing that can
// move the target's score is the recalculation this is testing. Severity 1 is
// 5 points over 90 days, and a freshly created user has no tenure, no activity
// and no vouches, so the composite is entirely the moderation component:
// (100 - 5) * 0.30 = 28.5, down from the 100 they were seeded with.
func TestBuild_ModerationPenaltyRecalculatesTrust(t *testing.T) {
	pool := testsupport.TestDB(t)
	deps := buildTestDeps(t, pool, true)
	ctx := startTrustWorker(t, deps)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("mod"), domain.RoleModerator, 90.0)
	const seeded = 100.0
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("warned"), domain.RoleMember, seeded)

	if _, err := deps.ModerationService.TakeAction(
		ctx, moderator.ID, target.ID, domain.ActionWarn, 1, "posting off-topic", nil,
	); err != nil {
		t.Fatalf("TakeAction() unexpected error: %v", err)
	}

	got := awaitTrustChange(t, deps, target.ID, seeded)

	const want = 28.5
	if diff := got - want; diff > 0.5 || diff < -0.5 {
		t.Errorf("recalculated trust = %v, want ~%v (tenure 0, activity 0, vouches 0, moderation (100-5)*0.30)", got, want)
	}
}

// A vouch changes the vouchee's voucher component, so receiving one must
// trigger a recalculation of the person who received it.
//
// The exact landing value is left unasserted: the voucher is queued too, and
// whether their own score has been recomputed by the time the vouchee's
// average-voucher-trust lookup runs decides between two legitimate results. The
// wiring claim under test is that the score moves at all.
func TestBuild_VouchRecalculatesTrust(t *testing.T) {
	pool := testsupport.TestDB(t)
	deps := buildTestDeps(t, pool, true)
	ctx := startTrustWorker(t, deps)

	voucher := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("voucher"), domain.RoleMember, 80.0)
	const seeded = 100.0
	vouchee := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("vouchee"), domain.RoleMember, seeded)

	if _, err := deps.VouchService.Vouch(ctx, voucher.ID, vouchee.ID); err != nil {
		t.Fatalf("Vouch() unexpected error: %v", err)
	}

	got := awaitTrustChange(t, deps, vouchee.ID, seeded)

	// Whatever the ordering, a user with no tenure, no activity and one vouch
	// cannot still be at 100, nor at the ceiling.
	if got >= seeded {
		t.Errorf("recalculated trust = %v, want it below the seeded %v", got, seeded)
	}
}

// Redis is optional. Without it Build leaves the queue unset, the enqueue calls
// return at their nil guard, and moderation still has to work — the degraded
// mode must be degraded, not broken.
func TestBuild_WithoutRedisModerationStillSucceeds(t *testing.T) {
	pool := testsupport.TestDB(t)
	deps := buildTestDeps(t, pool, false)

	if deps.TrustWorker != nil {
		t.Error("Build returned a TrustWorker without a Redis client")
	}

	ctx := context.Background()
	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("mod-noredis"), domain.RoleModerator, 90.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("warned-noredis"), domain.RoleMember, 100.0)

	if _, err := deps.ModerationService.TakeAction(
		ctx, moderator.ID, target.ID, domain.ActionWarn, 1, "posting off-topic", nil,
	); err != nil {
		t.Fatalf("TakeAction() without Redis: %v", err)
	}

	voucher := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("voucher-noredis"), domain.RoleMember, 80.0)
	vouchee := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("vouchee-noredis"), domain.RoleMember, 50.0)
	if _, err := deps.VouchService.Vouch(ctx, voucher.ID, vouchee.ID); err != nil {
		t.Fatalf("Vouch() without Redis: %v", err)
	}
}

// The P0, end to end: a mute must survive the recalculation its own penalty
// triggers.
//
// This asserts on both halves so that it can only pass for the right reason.
// The mute is enforced by domain.User.MutedUntil, which CanPost consults
// independently of the score, so after the recalculation the trust score must
// be back ABOVE the posting threshold while CanPost is still false. Asserting
// only "cannot post" would pass just as well if the score were being clamped
// again, or if enforcement had quietly gone back to writing a low number —
// which is exactly the interim mechanism this replaced.
func TestBuild_MuteSurvivesTrustRecalculation(t *testing.T) {
	pool := testsupport.TestDB(t)
	deps := buildTestDeps(t, pool, true)
	ctx := startTrustWorker(t, deps)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("mod-mute"), domain.RoleModerator, 90.0)
	const seeded = 100.0
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("muted"), domain.RoleMember, seeded)

	// The target has to be an established member. A brand-new account
	// recomputes to 22.5 — already under the threshold — so the mute would
	// appear to hold for the wrong reason. Two years of tenure lifts the
	// recomputed score to 37.5, above the threshold, which is the case that
	// needs MutedUntil to be doing the work.
	if _, err := pool.Exec(ctx,
		"UPDATE users SET joined_at = $2 WHERE id = $1", target.ID, time.Now().AddDate(-2, 0, 0),
	); err != nil {
		t.Fatalf("backdating target tenure: %v", err)
	}

	oneHour := int64(3600)
	if _, err := deps.ModerationService.TakeAction(
		ctx, moderator.ID, target.ID, domain.ActionMute, 3, "off-topic", &oneHour,
	); err != nil {
		t.Fatalf("TakeAction(mute) unexpected error: %v", err)
	}

	// Muting no longer touches the score, so any movement is the recalculation.
	got := awaitTrustChange(t, deps, target.ID, seeded)

	user, err := deps.UserService.GetByID(ctx, target.ID)
	if err != nil {
		t.Fatalf("reading back muted user: %v", err)
	}
	now := time.Now()

	if !user.IsMuted(now) {
		t.Fatalf("MutedUntil = %v: the mute did not survive the recalculation", user.MutedUntil)
	}
	if user.CanPost(now) {
		t.Errorf("CanPost() = true for a muted user (trust %v)", got)
	}
	// The half that proves it is MutedUntil doing the work, not a low score.
	if got < domain.PostingThreshold {
		t.Errorf("trust = %v, below the posting threshold %v — this test would pass even "+
			"if MutedUntil were broken, so the score is being used to enforce the mute again",
			got, domain.PostingThreshold)
	}
}

// PUT /admin/config has to reach a handler that actually holds a transactor.
// The trap this guards is a variadic or optional wiring that keeps everything
// compiling while the handler receives nothing at runtime: the endpoint would
// still answer 204, and the atomicity it promises would silently not exist.
// Driving the route through the options Build really returns is the only way to
// tell those apart.
func TestBuild_ConfigUpdateIsWiredThroughTheRealRouter(t *testing.T) {
	pool := testsupport.TestDB(t)
	deps := buildTestDeps(t, pool, false)

	council := &domain.User{
		ID:       uuid.Must(uuid.NewV7()).String(),
		Role:     domain.RoleCouncil,
		IsActive: true,
	}
	// Applied after Build's own WithAuth, so it wins: the route is council-only
	// and this test is about the handler behind the guard, not the guard.
	opts := append(deps.ServerOptions, server.WithAuth(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(middleware.WithUser(r.Context(), council)))
		})
	}))
	srv := server.New(deps.Config, pool, testsupport.DiscardLogger(), opts...)

	body := strings.NewReader(`{"town_name":"Bellville","accent_color":"#c62828"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config", body)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}

	stored, err := deps.ConfigRepo.ListTownConfig(context.Background())
	if err != nil {
		t.Fatalf("reading back town config: %v", err)
	}
	if stored["town_name"] != "Bellville" {
		t.Errorf("town_name = %q, want %q", stored["town_name"], "Bellville")
	}
	if stored["accent_color"] != "#c62828" {
		t.Errorf("accent_color = %q, want %q", stored["accent_color"], "#c62828")
	}
}

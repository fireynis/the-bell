//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/testsupport"
)

// applySuspend suspends targetID for an hour over the real moderation endpoint,
// so the state these tests then lift is the state a moderator actually
// produces rather than one written straight into the users table.
func applySuspend(t *testing.T, h http.Handler, targetID string) {
	t.Helper()

	body := mustJSON(t, map[string]any{
		"target_user_id":   targetID,
		"action_type":      "suspend",
		"severity":         4,
		"reason":           "repeated harassment after a warning",
		"duration_seconds": 3600,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/moderation/actions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("applying suspension: status %d: %s", rec.Code, rec.Body.String())
	}
}

// liftSuspension issues the release and returns the recorder.
func liftSuspension(h http.Handler, targetID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/moderation/users/"+targetID+"/suspension", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// readSuspensionStatus asks the moderator-facing read what it knows.
func readSuspensionStatus(t *testing.T, h http.Handler, targetID string) (status int, suspendedUntil string) {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/moderation/users/"+targetID+"/suspension", nil))
	if rec.Code != http.StatusOK {
		return rec.Code, ""
	}

	var body struct {
		SuspendedUntil string `json:"suspended_until"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding suspension status: %v", err)
	}
	return rec.Code, body.SuspendedUntil
}

// suspensionState reads the three columns that decide a suspended member's
// standing, straight from Postgres. is_active is read as the raw column here,
// not through a response: the API's is_active field folds the suspension in, and
// what these tests need to know is whether the column itself was written.
func suspensionState(t *testing.T, pool *pgxpool.Pool, userID string) (until *time.Time, isActive bool, trust float64) {
	t.Helper()

	if err := pool.QueryRow(context.Background(),
		`SELECT suspended_until, is_active, trust_score FROM users WHERE id = $1`, userID,
	).Scan(&until, &isActive, &trust); err != nil {
		t.Fatalf("reading suspension state: %v", err)
	}
	return until, isActive, trust
}

// A suspension applied in error had to run its full course: 00019 gave it an
// expiry to lapse from, but nothing could end it early. This is the whole point
// of the endpoint, end to end.
func TestLiftSuspensionClearsItAndLeavesTrustAlone(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("suspmod"), domain.RoleModerator, 90.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("susptarget"), domain.RoleMember, 80.0)
	h := testServer(t, pool, moderator).Handler()

	applySuspend(t, h, target.ID)

	suspendedUntil, activeWhileSuspended, trustWhileSuspended := suspensionState(t, pool, target.ID)
	if suspendedUntil == nil {
		t.Fatal("the suspension did not land, so lifting it would prove nothing")
	}
	// The enforcement is the expiry, not the flag — the whole repair 00019 made.
	if !activeWhileSuspended {
		t.Fatal("is_active was written by the suspension; it is meant to stay untouched")
	}
	actionsBefore := countRows(t, pool,
		`SELECT COUNT(*) FROM moderation_actions WHERE target_user_id = $1`, target.ID)
	penaltiesBefore := countRows(t, pool,
		`SELECT COUNT(*) FROM trust_penalties WHERE user_id = $1`, target.ID)

	if rec := liftSuspension(h, target.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("lifting suspension: status %d, want %d: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}

	after, activeAfter, trustAfter := suspensionState(t, pool, target.ID)
	if after != nil {
		t.Errorf("suspended_until = %v, want NULL", *after)
	}
	// And the lift must not write it either. Setting it TRUE here would
	// reactivate an account deactivated for some entirely separate reason.
	if !activeAfter {
		t.Error("is_active was written by the lift")
	}

	// Releasing somebody is mercy, not a reward: the suspend action's own
	// severity-4 penalty stays exactly where it was.
	if trustAfter != trustWhileSuspended {
		t.Errorf("trust score moved from %v to %v; lifting a suspension must not touch it",
			trustWhileSuspended, trustAfter)
	}
	if got := countRows(t, pool, `SELECT COUNT(*) FROM trust_penalties WHERE user_id = $1`, target.ID); got != penaltiesBefore {
		t.Errorf("trust penalties = %d, want the %d that were already there", got, penaltiesBefore)
	}
	if got := countRows(t, pool, `SELECT COUNT(*) FROM moderation_actions WHERE target_user_id = $1`, target.ID); got != actionsBefore {
		t.Errorf("moderation actions = %d, want the %d already recorded", got, actionsBefore)
	}
}

// The suspension is only real if it stops the member, and the lift is only real
// if they come back. Both halves go over HTTP against the wired server.
func TestLiftSuspensionRestoresPosting(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("suspmod2"), domain.RoleModerator, 90.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("susptarget2"), domain.RoleMember, 80.0)
	modHandler := testServer(t, pool, moderator).Handler()

	applySuspend(t, modHandler, target.ID)

	post := func(user *domain.User, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/posts",
			bytes.NewReader(mustJSON(t, map[string]string{"body": body})))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		testServer(t, pool, user).Handler().ServeHTTP(rec, req)
		return rec
	}

	if rec := post(freshUser(t, pool, target.ID), "still here"); rec.Code != http.StatusForbidden {
		t.Fatalf("suspended user posting: status %d, want %d: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}

	if rec := liftSuspension(modHandler, target.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("lifting suspension: status %d: %s", rec.Code, rec.Body.String())
	}

	if rec := post(freshUser(t, pool, target.ID), "back after the appeal"); rec.Code != http.StatusCreated {
		t.Fatalf("released user posting: status %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

// Lifting a suspension nobody has is not an error: the caller asked for a state
// and the state holds. A moderator who clicks twice, or two moderators handling
// one appeal, must not see a failure for work that is done.
func TestLiftSuspensionIsIdempotent(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("suspmod3"), domain.RoleModerator, 90.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("susptarget3"), domain.RoleMember, 80.0)
	h := testServer(t, pool, moderator).Handler()

	// Never suspended at all.
	if rec := liftSuspension(h, target.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("lifting a suspension that was never applied: status %d, want %d: %s",
			rec.Code, http.StatusNoContent, rec.Body.String())
	}

	applySuspend(t, h, target.ID)
	for i := range 2 {
		if rec := liftSuspension(h, target.ID); rec.Code != http.StatusNoContent {
			t.Fatalf("lift %d: status %d, want %d: %s", i+1, rec.Code, http.StatusNoContent, rec.Body.String())
		}
	}

	if until, _, _ := suspensionState(t, pool, target.ID); until != nil {
		t.Errorf("suspended_until = %v, want NULL", *until)
	}
}

// A mistyped id is not the same as a user who happens not to be suspended.
func TestLiftSuspensionUnknownUserIsNotFound(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("suspmod4"), domain.RoleModerator, 90.0)
	h := testServer(t, pool, moderator).Handler()

	if rec := liftSuspension(h, "00000000-0000-0000-0000-000000000000"); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// The route inherits the group's moderator guard rather than declaring its own,
// so this holds that inheritance open. The suspended member is the case worth
// naming: RequireActive already stops them, and this pins that a member with no
// suspension of their own cannot release anybody either.
func TestLiftSuspensionRefusesMembers(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("suspmod5"), domain.RoleModerator, 90.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("susptarget5"), domain.RoleMember, 80.0)
	bystander := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("suspbystander"), domain.RoleMember, 80.0)
	applySuspend(t, testServer(t, pool, moderator).Handler(), target.ID)

	for _, tt := range []struct {
		name   string
		caller *domain.User
	}{
		{"another member", bystander},
		{"the suspended member themselves", freshUser(t, pool, target.ID)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := liftSuspension(testServer(t, pool, tt.caller).Handler(), target.ID)
			if rec.Code != http.StatusForbidden {
				t.Errorf("status = %d, want %d: %s", rec.Code, http.StatusForbidden, rec.Body.String())
			}
		})
	}

	if until, _, _ := suspensionState(t, pool, target.ID); until == nil {
		t.Error("the suspension was lifted by a caller with no moderator role")
	}
}

// The moderator-facing read is what makes the release affordance honest: the
// suspend action left in the audit trail keeps its original expires_at forever,
// and is_active says nothing about when a suspension ends.
func TestSuspensionStatusTracksTheSuspensionAndTheLift(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("suspstatusmod"), domain.RoleModerator, 90.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("suspstatustarget"), domain.RoleMember, 80.0)
	h := testServer(t, pool, moderator).Handler()

	if status, until := readSuspensionStatus(t, h, target.ID); status != http.StatusOK || until != "" {
		t.Fatalf("before the suspension: status %d, suspended_until %q; want 200 and no expiry", status, until)
	}

	applySuspend(t, h, target.ID)

	status, until := readSuspensionStatus(t, h, target.ID)
	if status != http.StatusOK {
		t.Fatalf("while suspended: status %d, want 200", status)
	}
	if until == "" {
		t.Fatal("while suspended: no suspended_until, so a moderator cannot see the suspension they just applied")
	}
	parsed, err := time.Parse(time.RFC3339, until)
	if err != nil {
		t.Fatalf("suspended_until %q is not RFC3339: %v", until, err)
	}
	if !parsed.After(time.Now()) {
		t.Errorf("suspended_until = %v, want a time still in the future", parsed)
	}

	if rec := liftSuspension(h, target.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("lifting suspension: status %d: %s", rec.Code, rec.Body.String())
	}

	if status, until := readSuspensionStatus(t, h, target.ID); status != http.StatusOK || until != "" {
		t.Errorf("after the lift: status %d, suspended_until %q; want 200 and no expiry", status, until)
	}
}

// suspended_until stays behind the moderator guard: it is on no response a
// member can reach.
func TestSuspensionStatusIsNotVisibleToMembers(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("suspstatusmod2"), domain.RoleModerator, 90.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("suspstatustarget2"), domain.RoleMember, 80.0)
	bystander := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("suspstatusbystander"), domain.RoleMember, 80.0)
	applySuspend(t, testServer(t, pool, moderator).Handler(), target.ID)

	if status, _ := readSuspensionStatus(t, testServer(t, pool, bystander).Handler(), target.ID); status != http.StatusForbidden {
		t.Errorf("member reading suspension status: status %d, want %d", status, http.StatusForbidden)
	}
}

// A suspension whose time has passed is not a suspension. Nothing sweeps the
// column, so a stale past timestamp sits in the row indefinitely and the read
// has to apply the same clock comparison the gates do.
func TestSuspensionStatusTreatsALapsedSuspensionAsNone(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("suspexpiredmod"), domain.RoleModerator, 90.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("suspexpiredtarget"), domain.RoleMember, 80.0)
	h := testServer(t, pool, moderator).Handler()

	// Written directly: a suspension that has already lapsed is exactly what the
	// row looks like once any real one runs its course.
	if _, err := pool.Exec(context.Background(),
		`UPDATE users SET suspended_until = NOW() - INTERVAL '1 hour' WHERE id = $1`, target.ID,
	); err != nil {
		t.Fatalf("writing a lapsed suspension: %v", err)
	}

	if status, until := readSuspensionStatus(t, h, target.ID); status != http.StatusOK || until != "" {
		t.Errorf("read of a lapsed suspension: status %d, suspended_until %q; want 200 and no expiry", status, until)
	}

	// And the release stays available and idempotent for that same user, which
	// lets a moderator clear a stale column without a special case.
	if rec := liftSuspension(h, target.ID); rec.Code != http.StatusNoContent {
		t.Errorf("lifting a lapsed suspension: status %d, want %d: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if until, _, _ := suspensionState(t, pool, target.ID); until != nil {
		t.Errorf("suspended_until = %v, want NULL", *until)
	}
}

// The point of the record. A suspension reaches the member as is_active being
// false; lifting it just makes that revert, so without this entry a member
// released early cannot tell that from a suspension that ran its full course.
func TestLiftSuspensionIsVisibleToTheReleasedMember(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("suspvis-mod"), domain.RoleModerator, 90.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("suspvis-target"), domain.RoleMember, 80.0)

	modHandler := testServer(t, pool, moderator).Handler()
	applySuspend(t, modHandler, target.ID)

	if rec := liftSuspension(modHandler, target.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("lifting suspension: status %d: %s", rec.Code, rec.Body.String())
	}

	released := freshUser(t, pool, target.ID)
	profile := readOwnProfile(t, testServer(t, pool, released).Handler())

	lifts, ok := profile["suspension_lifts"].([]any)
	if !ok || len(lifts) != 1 {
		t.Fatalf("suspension_lifts = %v, want exactly one lift", profile["suspension_lifts"])
	}
	lift, ok := lifts[0].(map[string]any)
	if !ok {
		t.Fatalf("suspension lift is %T, want an object", lifts[0])
	}
	if lift["lifted_at"] == nil {
		t.Error("the lift does not say when it happened")
	}
	// The suspension ran an hour; the record keeps the expiry that was
	// destroyed, which exists nowhere else once suspended_until is cleared.
	if lift["previous_suspended_until"] == nil {
		t.Error("the lift does not say what the suspension would have run to")
	}
	// A member is not shown which moderator acted.
	if _, named := lift["moderator_id"]; named {
		t.Errorf("the member view names the moderator: %v", lift["moderator_id"])
	}
	// The two lists share one table, and only relief_type keeps them apart.
	if _, crossed := profile["mute_lifts"]; crossed {
		t.Errorf("mute_lifts = %v for a member who only had a suspension lifted", profile["mute_lifts"])
	}
}

// The relief is a row, not a log line: queryable, typed, and carrying the
// suspended_until the lift destroyed.
func TestLiftSuspensionRecordsAQueryableRelief(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("susprow-mod"), domain.RoleModerator, 90.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("susprow-target"), domain.RoleMember, 80.0)
	h := testServer(t, pool, moderator).Handler()

	applySuspend(t, h, target.ID)
	suspendedUntil, _, _ := suspensionState(t, pool, target.ID)
	if suspendedUntil == nil {
		t.Fatal("the suspension did not land")
	}

	if rec := liftSuspension(h, target.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("lifting suspension: status %d: %s", rec.Code, rec.Body.String())
	}

	var (
		reliefType string
		previous   *time.Time
		wasInForce bool
		modID      string
	)
	err := pool.QueryRow(context.Background(),
		`SELECT relief_type, previous_expires_at, was_in_force, moderator_id
		   FROM moderation_reliefs WHERE target_user_id = $1`, target.ID,
	).Scan(&reliefType, &previous, &wasInForce, &modID)
	if err != nil {
		t.Fatalf("reading the relief row: %v", err)
	}

	if reliefType != "suspension_lift" {
		t.Errorf("relief_type = %q, want %q", reliefType, "suspension_lift")
	}
	if !wasInForce {
		t.Error("was_in_force is false for a suspension that was still running")
	}
	if modID != moderator.ID {
		t.Errorf("moderator_id = %q, want %q", modID, moderator.ID)
	}
	if previous == nil {
		t.Fatal("previous_expires_at is NULL; the destroyed suspension expiry was not kept")
	}
	if !previous.Equal(*suspendedUntil) {
		t.Errorf("previous_expires_at = %v, want the suspended_until that was cleared, %v",
			*previous, *suspendedUntil)
	}

	// Still no moderation_actions row: the relief is a separate table precisely
	// so the release does not have to claim a severity.
	if n := countRows(t, pool,
		`SELECT COUNT(*) FROM moderation_actions WHERE target_user_id = $1 AND action_type = 'suspend'`,
		target.ID); n != 1 {
		t.Errorf("%d suspend actions, want just the original one", n)
	}
}

// An idempotent lift against somebody who was not suspended is a real event and
// is recorded, but flagged — and the member is not told they were released from
// a suspension they never had.
func TestLiftSuspensionOnAnUnsuspendedMemberIsRecordedButNotShown(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("suspnoop-mod"), domain.RoleModerator, 90.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("suspnoop-target"), domain.RoleMember, 80.0)

	if rec := liftSuspension(testServer(t, pool, moderator).Handler(), target.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("lifting a suspension nobody had: status %d: %s", rec.Code, rec.Body.String())
	}

	var (
		wasInForce bool
		reliefType string
	)
	err := pool.QueryRow(context.Background(),
		`SELECT was_in_force, relief_type FROM moderation_reliefs WHERE target_user_id = $1`,
		target.ID).Scan(&wasInForce, &reliefType)
	if err != nil {
		t.Fatalf("the lift was not recorded at all: %v", err)
	}
	if wasInForce {
		t.Error("was_in_force is true for a member who was never suspended")
	}
	if reliefType != "suspension_lift" {
		t.Errorf("relief_type = %q, want %q", reliefType, "suspension_lift")
	}

	profile := readOwnProfile(t, testServer(t, pool, freshUser(t, pool, target.ID)).Handler())
	if _, ok := profile["suspension_lifts"]; ok {
		t.Errorf("suspension_lifts = %v; a member who was never suspended must not be told they were released",
			profile["suspension_lifts"])
	}
}

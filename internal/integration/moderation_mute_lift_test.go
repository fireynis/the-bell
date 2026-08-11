//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/testsupport"
)

// applyMute mutes targetID for an hour over the real moderation endpoint, so
// the state these tests then lift is the state a moderator actually produces
// rather than one written straight into the users table.
func applyMute(t *testing.T, h http.Handler, targetID string) {
	t.Helper()

	body := mustJSON(t, map[string]any{
		"target_user_id":   targetID,
		"action_type":      "mute",
		"severity":         3,
		"reason":           "posting the same thing repeatedly",
		"duration_seconds": 3600,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/moderation/actions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("applying mute: status %d: %s", rec.Code, rec.Body.String())
	}
}

// liftMute issues the un-mute and returns the recorder.
func liftMute(h http.Handler, targetID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/moderation/users/"+targetID+"/mute", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// readMuteStatus asks the moderator-facing read what it knows about a mute.
func readMuteStatus(t *testing.T, h http.Handler, targetID string) (status int, mutedUntil string) {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/moderation/users/"+targetID+"/mute", nil))
	if rec.Code != http.StatusOK {
		return rec.Code, ""
	}

	var body struct {
		MutedUntil string `json:"muted_until"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding mute status: %v", err)
	}
	return rec.Code, body.MutedUntil
}

// muteState reads the two columns that decide whether someone is muted and what
// their standing is, straight from Postgres rather than through any response
// shape — muted_until is deliberately absent from every response but the
// caller's own profile, so the database is the only place to check it.
func muteState(t *testing.T, pool *pgxpool.Pool, userID string) (mutedUntil *time.Time, trust float64) {
	t.Helper()

	var until *time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT muted_until, trust_score FROM users WHERE id = $1`, userID,
	).Scan(&until, &trust); err != nil {
		t.Fatalf("reading mute state: %v", err)
	}
	return until, trust
}

// countRows counts the audit rows a lift must not add to.
func countRows(t *testing.T, pool *pgxpool.Pool, query, userID string) int {
	t.Helper()

	var n int
	if err := pool.QueryRow(context.Background(), query, userID).Scan(&n); err != nil {
		t.Fatalf("counting rows: %v", err)
	}
	return n
}

// freshUser re-reads a user from Postgres. The harness's auth middleware injects
// one fixed *domain.User, so a server built before the mute keeps handing out a
// stale copy; anything asserting on what the target may now do has to be given
// the row as it stands.
func freshUser(t *testing.T, pool *pgxpool.Pool, userID string) *domain.User {
	t.Helper()

	u, err := postgres.NewUserRepo(postgres.New(pool)).GetUserByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("re-reading user: %v", err)
	}
	return u
}

// A mute applied in error, or one shortened after an appeal, had to run its
// full course: SetUserMutedUntil(ctx, id, nil) existed, was tested, and nothing
// reached it. This is the whole point of the endpoint, end to end.
func TestLiftMuteClearsTheMuteAndLeavesTrustAlone(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("liftmod"), domain.RoleModerator, 90.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("lifttarget"), domain.RoleMember, 80.0)
	h := testServer(t, pool, moderator).Handler()

	applyMute(t, h, target.ID)

	mutedUntil, trustWhileMuted := muteState(t, pool, target.ID)
	if mutedUntil == nil {
		t.Fatal("the mute did not land, so lifting it would prove nothing")
	}
	actionsBefore := countRows(t, pool,
		`SELECT COUNT(*) FROM moderation_actions WHERE target_user_id = $1`, target.ID)
	penaltiesBefore := countRows(t, pool,
		`SELECT COUNT(*) FROM trust_penalties WHERE user_id = $1`, target.ID)

	if rec := liftMute(h, target.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("lifting mute: status %d, want %d: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}

	after, trustAfter := muteState(t, pool, target.ID)
	if after != nil {
		t.Errorf("muted_until = %v, want NULL", *after)
	}

	// Requirement made explicit: a mute has not touched trust since that bug
	// was fixed, so releasing one must not either — it is mercy, not a reward.
	if trustAfter != trustWhileMuted {
		t.Errorf("trust score moved from %v to %v; lifting a mute must not touch it", trustWhileMuted, trustAfter)
	}
	if got := countRows(t, pool, `SELECT COUNT(*) FROM trust_penalties WHERE user_id = $1`, target.ID); got != penaltiesBefore {
		t.Errorf("trust penalties = %d, want the %d that were already there", got, penaltiesBefore)
	}

	// No moderation_actions row: every severity in that table names a trust
	// penalty that PropagatePenalties walks the vouch graph with, and there is
	// none meaning "no punishment".
	if got := countRows(t, pool, `SELECT COUNT(*) FROM moderation_actions WHERE target_user_id = $1`, target.ID); got != actionsBefore {
		t.Errorf("moderation actions = %d, want the %d already recorded", got, actionsBefore)
	}
}

// The mute is only real if it stops the posting, and the lift is only real if
// the posting comes back. Both halves go over HTTP against the wired server.
func TestLiftMuteRestoresPosting(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("liftmod2"), domain.RoleModerator, 90.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("lifttarget2"), domain.RoleMember, 80.0)
	modHandler := testServer(t, pool, moderator).Handler()

	applyMute(t, modHandler, target.ID)

	post := func(user *domain.User, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/posts",
			bytes.NewReader(mustJSON(t, map[string]string{"body": body})))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		testServer(t, pool, user).Handler().ServeHTTP(rec, req)
		return rec
	}

	if rec := post(freshUser(t, pool, target.ID), "still talking"); rec.Code != http.StatusForbidden {
		t.Fatalf("muted user posting: status %d, want %d: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}

	if rec := liftMute(modHandler, target.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("lifting mute: status %d: %s", rec.Code, rec.Body.String())
	}

	if rec := post(freshUser(t, pool, target.ID), "back after the appeal"); rec.Code != http.StatusCreated {
		t.Fatalf("released user posting: status %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

// Lifting a mute nobody has is not an error. The caller asked for a state and
// the state holds — the same answer the reaction DELETE gives for a reaction
// that was never left. A moderator who clicks twice, or two moderators handling
// one appeal, must not see a failure for work that is done.
func TestLiftMuteIsIdempotent(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("liftmod3"), domain.RoleModerator, 90.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("lifttarget3"), domain.RoleMember, 80.0)
	h := testServer(t, pool, moderator).Handler()

	// Never muted at all.
	if rec := liftMute(h, target.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("lifting a mute that was never applied: status %d, want %d: %s",
			rec.Code, http.StatusNoContent, rec.Body.String())
	}

	applyMute(t, h, target.ID)
	for i := range 2 {
		if rec := liftMute(h, target.ID); rec.Code != http.StatusNoContent {
			t.Fatalf("lift %d: status %d, want %d: %s", i+1, rec.Code, http.StatusNoContent, rec.Body.String())
		}
	}

	if until, _ := muteState(t, pool, target.ID); until != nil {
		t.Errorf("muted_until = %v, want NULL", *until)
	}
}

// A mistyped id is not the same as a user who happens not to be muted, and
// answering 204 for it would tell a moderator someone had been released who
// does not exist.
func TestLiftMuteUnknownUserIsNotFound(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("liftmod4"), domain.RoleModerator, 90.0)
	h := testServer(t, pool, moderator).Handler()

	if rec := liftMute(h, "00000000-0000-0000-0000-000000000000"); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// The route inherits the group's moderator guard rather than declaring its own,
// so this holds that inheritance open: a member must not be able to release
// anyone, and a muted member must not be able to release themselves.
func TestLiftMuteRefusesMembers(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("liftmod5"), domain.RoleModerator, 90.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("lifttarget5"), domain.RoleMember, 80.0)
	bystander := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("liftbystander"), domain.RoleMember, 80.0)
	applyMute(t, testServer(t, pool, moderator).Handler(), target.ID)

	for _, tt := range []struct {
		name   string
		caller *domain.User
	}{
		{"another member", bystander},
		{"the muted member themselves", freshUser(t, pool, target.ID)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := liftMute(testServer(t, pool, tt.caller).Handler(), target.ID)
			if rec.Code != http.StatusForbidden {
				t.Errorf("status = %d, want %d: %s", rec.Code, http.StatusForbidden, rec.Body.String())
			}
		})
	}

	if until, _ := muteState(t, pool, target.ID); until == nil {
		t.Error("the mute was lifted by a caller with no moderator role")
	}
}

// The moderator-facing read is what makes the un-mute affordance honest: a
// moderator's view of another user carries no muted_until anywhere else, and
// the mute action left in the audit trail keeps claiming a mute long after one
// is lifted.
func TestMuteStatusTracksTheMuteAndTheLift(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("statusmod"), domain.RoleModerator, 90.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("statustarget"), domain.RoleMember, 80.0)
	h := testServer(t, pool, moderator).Handler()

	if status, until := readMuteStatus(t, h, target.ID); status != http.StatusOK || until != "" {
		t.Fatalf("before the mute: status %d, muted_until %q; want 200 and no expiry", status, until)
	}

	applyMute(t, h, target.ID)

	status, until := readMuteStatus(t, h, target.ID)
	if status != http.StatusOK {
		t.Fatalf("while muted: status %d, want 200", status)
	}
	if until == "" {
		t.Fatal("while muted: no muted_until, so a moderator cannot see the mute they just applied")
	}
	parsed, err := time.Parse(time.RFC3339, until)
	if err != nil {
		t.Fatalf("muted_until %q is not RFC3339: %v", until, err)
	}
	if !parsed.After(time.Now()) {
		t.Errorf("muted_until = %v, want a time still in the future", parsed)
	}

	if rec := liftMute(h, target.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("lifting mute: status %d: %s", rec.Code, rec.Body.String())
	}

	if status, until := readMuteStatus(t, h, target.ID); status != http.StatusOK || until != "" {
		t.Errorf("after the lift: status %d, muted_until %q; want 200 and no expiry", status, until)
	}
}

// muted_until stays off every response a non-moderator can reach. This is the
// rule the read had to be placed to preserve, so it is asserted rather than
// assumed: the moderation route group refuses the member outright, and the
// public profile carries no such field to begin with.
func TestMuteStatusIsNotVisibleToMembers(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("statusmod2"), domain.RoleModerator, 90.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("statustarget2"), domain.RoleMember, 80.0)
	bystander := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("statusbystander"), domain.RoleMember, 80.0)
	applyMute(t, testServer(t, pool, moderator).Handler(), target.ID)

	memberHandler := testServer(t, pool, bystander).Handler()
	if status, _ := readMuteStatus(t, memberHandler, target.ID); status != http.StatusForbidden {
		t.Errorf("member reading mute status: status %d, want %d", status, http.StatusForbidden)
	}

	rec := httptest.NewRecorder()
	memberHandler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/users/"+target.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("reading public profile: status %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "muted_until") {
		t.Errorf("the public profile carries muted_until: %s", rec.Body.String())
	}
}

// The read and the un-mute have to agree about what "not muted" means. If the
// read omits the field, the DELETE for that same user must still answer 204
// rather than 404: the two are one story told twice, and a moderator acting on
// what the read told them must not be met with a refusal.
func TestMuteStatusAndLiftAgreeOnAnUnmutedUser(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("agreemod"), domain.RoleModerator, 90.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("agreetarget"), domain.RoleMember, 80.0)
	h := testServer(t, pool, moderator).Handler()

	status, until := readMuteStatus(t, h, target.ID)
	if status != http.StatusOK || until != "" {
		t.Fatalf("read: status %d, muted_until %q; want 200 and no expiry", status, until)
	}

	if rec := liftMute(h, target.ID); rec.Code != http.StatusNoContent {
		t.Errorf("lift of the same user: status %d, want %d: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
}

// A mute whose time has passed is not a mute. domain.User.IsMuted compares
// muted_until against the clock and nothing sweeps the column, so a stale past
// timestamp sits in the row indefinitely — the read has to apply the same
// comparison rather than hand back a past expiry the banner would render as
// "muted until [yesterday]".
func TestMuteStatusTreatsAnExpiredMuteAsNoMute(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("expiredmod"), domain.RoleModerator, 90.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("expiredtarget"), domain.RoleMember, 80.0)
	h := testServer(t, pool, moderator).Handler()

	// Written directly: a mute that has already lapsed is exactly what the row
	// looks like once any real mute runs its course, and no endpoint produces
	// one on demand.
	if _, err := pool.Exec(context.Background(),
		`UPDATE users SET muted_until = NOW() - INTERVAL '1 hour' WHERE id = $1`, target.ID,
	); err != nil {
		t.Fatalf("writing an expired mute: %v", err)
	}

	if status, until := readMuteStatus(t, h, target.ID); status != http.StatusOK || until != "" {
		t.Errorf("read of an expired mute: status %d, muted_until %q; want 200 and no expiry", status, until)
	}

	// And the un-mute stays available and idempotent for that same user, which
	// is what lets a moderator clear a stale column without a special case.
	if rec := liftMute(h, target.ID); rec.Code != http.StatusNoContent {
		t.Errorf("lifting an expired mute: status %d, want %d: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if until, _ := muteState(t, pool, target.ID); until != nil {
		t.Errorf("muted_until = %v, want NULL", *until)
	}
}

// readOwnProfile fetches a member's own profile as that member.
func readOwnProfile(t *testing.T, h http.Handler) map[string]any {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("reading own profile: status %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding own profile: %v", err)
	}
	return body
}

// The point of the whole record: a member who was muted and then released can
// see that it happened. Before moderation_reliefs existed a lift left only a log
// line — not queryable, and invisible to the person it concerned, whose mute
// simply vanished from their profile with nothing to say why.
//
// Every hop is real: the mute and the lift go over the moderation endpoints as a
// moderator, and the profile is read over HTTP as the member.
func TestLiftMuteIsVisibleToTheReleasedMember(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("liftvis-mod"), domain.RoleModerator, 90.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("liftvis-target"), domain.RoleMember, 80.0)

	modHandler := testServer(t, pool, moderator).Handler()
	applyMute(t, modHandler, target.ID)

	// While muted, the member sees the mute and no lift.
	muted := freshUser(t, pool, target.ID)
	beforeLift := readOwnProfile(t, testServer(t, pool, muted).Handler())
	if beforeLift["muted_until"] == nil {
		t.Fatal("the muted member cannot see their own mute, so the lift would prove nothing")
	}
	if _, ok := beforeLift["mute_lifts"]; ok {
		t.Errorf("mute_lifts is present before any lift: %v", beforeLift["mute_lifts"])
	}

	if rec := liftMute(modHandler, target.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("lifting mute: status %d: %s", rec.Code, rec.Body.String())
	}

	released := freshUser(t, pool, target.ID)
	afterLift := readOwnProfile(t, testServer(t, pool, released).Handler())

	// The mute is gone from the profile — which is exactly why something else
	// has to say it was lifted.
	if afterLift["muted_until"] != nil {
		t.Errorf("muted_until = %v after the lift, want it absent", afterLift["muted_until"])
	}

	lifts, ok := afterLift["mute_lifts"].([]any)
	if !ok || len(lifts) != 1 {
		t.Fatalf("mute_lifts = %v, want exactly one lift", afterLift["mute_lifts"])
	}
	lift, ok := lifts[0].(map[string]any)
	if !ok {
		t.Fatalf("mute lift is %T, want an object", lifts[0])
	}
	if lift["lifted_at"] == nil {
		t.Error("the lift does not say when it happened")
	}
	// The mute the member was released from ran an hour; the record keeps the
	// expiry that was destroyed, which exists nowhere else once muted_until is
	// cleared.
	if lift["previous_muted_until"] == nil {
		t.Error("the lift does not say what the mute would have run to")
	}
	// A member is not shown which moderator acted; that is a policy question of
	// its own and no member-facing response answers it today.
	if _, named := lift["moderator_id"]; named {
		t.Errorf("the member view names the moderator: %v", lift["moderator_id"])
	}
}

// The relief is a row, not a log line: it is queryable, and it carries the
// muted_until that the lift destroyed.
func TestLiftMuteRecordsAQueryableRelief(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("liftrow-mod"), domain.RoleModerator, 90.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("liftrow-target"), domain.RoleMember, 80.0)
	h := testServer(t, pool, moderator).Handler()

	applyMute(t, h, target.ID)
	mutedUntil, _ := muteState(t, pool, target.ID)
	if mutedUntil == nil {
		t.Fatal("the mute did not land")
	}

	if rec := liftMute(h, target.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("lifting mute: status %d: %s", rec.Code, rec.Body.String())
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

	if reliefType != "mute_lift" {
		t.Errorf("relief_type = %q, want %q", reliefType, "mute_lift")
	}
	if !wasInForce {
		t.Error("was_in_force is false for a mute that was still running")
	}
	if modID != moderator.ID {
		t.Errorf("moderator_id = %q, want %q", modID, moderator.ID)
	}
	if previous == nil {
		t.Fatal("previous_expires_at is NULL; the destroyed mute expiry was not kept")
	}
	if !previous.Equal(*mutedUntil) {
		t.Errorf("previous_expires_at = %v, want the muted_until that was cleared, %v", *previous, *mutedUntil)
	}

	// Still no moderation_actions row: the relief is a separate table precisely
	// so the release does not have to claim a severity.
	if n := countRows(t, pool,
		`SELECT COUNT(*) FROM moderation_actions WHERE target_user_id = $1 AND action_type = 'mute'`,
		target.ID); n != 1 {
		t.Errorf("%d mute actions, want just the original one", n)
	}
}

// An idempotent lift against somebody who was not muted is a real event and is
// recorded, but flagged — and the member is not told they were released from a
// mute they never had.
func TestLiftMuteOnAnUnmutedMemberIsRecordedButNotShown(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("liftnoop-mod"), domain.RoleModerator, 90.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("liftnoop-target"), domain.RoleMember, 80.0)

	if rec := liftMute(testServer(t, pool, moderator).Handler(), target.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("lifting a mute nobody had: status %d: %s", rec.Code, rec.Body.String())
	}

	var wasInForce bool
	err := pool.QueryRow(context.Background(),
		`SELECT was_in_force FROM moderation_reliefs WHERE target_user_id = $1`, target.ID).Scan(&wasInForce)
	if err != nil {
		t.Fatalf("the lift was not recorded at all: %v", err)
	}
	if wasInForce {
		t.Error("was_in_force is true for a member who was never muted")
	}

	profile := readOwnProfile(t, testServer(t, pool, freshUser(t, pool, target.ID)).Handler())
	if _, ok := profile["mute_lifts"]; ok {
		t.Errorf("mute_lifts = %v; a member who was never muted must not be told they were released",
			profile["mute_lifts"])
	}
}

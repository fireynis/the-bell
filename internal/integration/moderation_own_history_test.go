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

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/testsupport"
)

// GET /api/v1/users/me/moderation-history end to end: a real moderator takes a
// real action over the real endpoint, the penalty really propagates through the
// vouch graph, and then the member reads back what they are entitled to see.
//
// The unit tests pin the stripping against mocks. What only this level can
// prove is that the route is actually registered where the guards let the right
// people through — a suspended member's account reads is_active = false out of
// Postgres, and every other authenticated route would turn them away.

const ownHistoryPath = "/api/v1/users/me/moderation-history"

// takeAction applies a moderation action over the HTTP endpoint, so the rows
// these tests read back are the rows a moderator actually produces.
func takeAction(t *testing.T, h http.Handler, body map[string]any) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/moderation/actions", bytes.NewReader(mustJSON(t, body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("taking action: status %d: %s", rec.Code, rec.Body.String())
	}
}

// ownHistoryEntry is the member-facing entry, decoded loosely: a field the
// server was supposed to strip shows up as an extra key rather than vanishing
// into a struct with no room for it.
type ownHistoryEntry map[string]any

// readOwnHistory calls the endpoint as whoever the given server authenticates
// as, returning the status, the decoded entries and the raw body.
func readOwnHistory(t *testing.T, h http.Handler) (int, []ownHistoryEntry, string) {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ownHistoryPath, nil))
	raw := rec.Body.String()
	if rec.Code != http.StatusOK {
		return rec.Code, nil, raw
	}

	var body struct {
		Actions []ownHistoryEntry `json:"actions"`
	}
	if err := json.NewDecoder(strings.NewReader(raw)).Decode(&body); err != nil {
		t.Fatalf("decoding own history: %v (body %s)", err, raw)
	}
	return rec.Code, body.Actions, raw
}

// A member warned for something could learn only that they were warned, and
// only if a moderator told them out of band: the reason, the trust it cost and
// when that cost fades all sat behind the moderator-only route. This is the
// whole point of the endpoint, end to end.
func TestOwnModerationHistoryTellsTheMemberWhatHappenedAndWhatItCost(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("ownhistmod"), domain.RoleModerator, 90.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("ownhisttarget"), domain.RoleMember, 80.0)

	takeAction(t, testServer(t, pool, moderator).Handler(), map[string]any{
		"target_user_id": target.ID,
		"action_type":    "warn",
		"severity":       1,
		"reason":         "posting the same thing repeatedly",
	})

	status, entries, raw := readOwnHistory(t, testServer(t, pool, freshUser(t, pool, target.ID)).Handler())
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", status, raw)
	}
	if len(entries) != 1 {
		t.Fatalf("%d entries, want 1; body: %s", len(entries), raw)
	}

	entry := entries[0]
	if entry["action"] != "warn" {
		t.Errorf("action = %v, want warn", entry["action"])
	}
	if entry["severity"] != 1.0 {
		t.Errorf("severity = %v, want 1", entry["severity"])
	}
	if entry["reason"] != "posting the same thing repeatedly" {
		t.Errorf("reason = %v, want the moderator's words verbatim", entry["reason"])
	}

	penalty, ok := entry["penalty"].(map[string]any)
	if !ok {
		t.Fatalf("penalty = %v, want the 5 points a minor warning costs", entry["penalty"])
	}
	if penalty["amount"] != 5.0 {
		t.Errorf("penalty.amount = %v, want 5", penalty["amount"])
	}
	// A minor warning decays over 90 days, so the member is told when it will
	// have faded rather than left to assume it is permanent.
	if decaysAt, _ := penalty["decays_at"].(string); decaysAt == "" {
		t.Errorf("penalty.decays_at is missing; a minor warning is not permanent: %v", penalty)
	}
}

// The moderator is not named, and neither is anyone whose standing the action
// also cost. The vouch edge here is what makes the second half real: without a
// voucher there are no propagated penalties to leak.
func TestOwnModerationHistoryNamesNeitherTheModeratorNorTheVouchers(t *testing.T) {
	pool := testsupport.TestDB(t)

	council := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("ownhistcouncil"), domain.RoleCouncil, 95.0)
	voucher := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("ownhistvoucher"), domain.RoleMember, 85.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("ownhistbanned"), domain.RoleMember, 80.0)

	svcs := newTestServices(t, pool)
	if _, err := svcs.VouchService.Vouch(context.Background(), voucher.ID, target.ID); err != nil {
		t.Fatalf("vouching: %v", err)
	}

	// A ban is the severity that propagates furthest — three hops — so it is
	// the action most likely to carry somebody else's penalty out with it.
	takeAction(t, testServer(t, pool, council).Handler(), map[string]any{
		"target_user_id": target.ID,
		"action_type":    "ban",
		"severity":       5,
		"reason":         "repeated harassment after two warnings",
	})

	status, entries, raw := readOwnHistory(t, testServer(t, pool, freshUser(t, pool, target.ID)).Handler())
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", status, raw)
	}
	if len(entries) != 1 {
		t.Fatalf("%d entries, want 1; body: %s", len(entries), raw)
	}

	if strings.Contains(raw, council.ID) {
		t.Errorf("the acting moderator's id reached the member: %s", raw)
	}
	if strings.Contains(raw, voucher.ID) {
		t.Errorf("a voucher's id reached the member: %s", raw)
	}
	if strings.Contains(raw, "moderator") {
		t.Errorf("the response carries a moderator field: %s", raw)
	}

	// The member's own penalty is present, and permanent: a ban's 100 points
	// never decay, which is said by decays_at being absent while penalty is not.
	penalty, ok := entries[0]["penalty"].(map[string]any)
	if !ok {
		t.Fatalf("penalty = %v, want the ban's 100 points", entries[0]["penalty"])
	}
	if penalty["amount"] != 100.0 {
		t.Errorf("penalty.amount = %v, want 100", penalty["amount"])
	}
	if _, present := penalty["decays_at"]; present {
		t.Errorf("penalty.decays_at is present for a ban: %v", penalty)
	}
}

// The auth seam, proved rather than asserted. The suspended member is turned
// away from the ordinary self view — which is what RequireActive would have
// done to this endpoint too — and is served their own history.
//
// Being told "account suspended" by the endpoint that exists to explain the
// suspension is the failure mode the skipActive guard was chosen to remove.
func TestOwnModerationHistoryIsReadableBySomebodySuspended(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("ownhistsuspmod"), domain.RoleModerator, 90.0)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("ownhistsusp"), domain.RoleMember, 80.0)

	takeAction(t, testServer(t, pool, moderator).Handler(), map[string]any{
		"target_user_id":   target.ID,
		"action_type":      "suspend",
		"severity":         4,
		"reason":           "harassing another member in the comments",
		"duration_seconds": 604800,
	})

	// Re-read from Postgres: the repository folds a live suspension into
	// is_active, so this is the user every guard in the chain will see.
	suspended := freshUser(t, pool, target.ID)
	if suspended.IsActive {
		t.Fatalf("target is still active after a suspension; the rest of this test proves nothing")
	}
	h := testServer(t, pool, suspended).Handler()

	// The control: an ordinary authenticated self view refuses them.
	profileRec := httptest.NewRecorder()
	h.ServeHTTP(profileRec, httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil))
	if profileRec.Code != http.StatusForbidden {
		t.Fatalf("GET /api/v1/users/me = %d, want 403 for a suspended member", profileRec.Code)
	}

	status, entries, raw := readOwnHistory(t, h)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; a suspended member must be able to read why: %s", status, raw)
	}
	if len(entries) != 1 {
		t.Fatalf("%d entries, want 1; body: %s", len(entries), raw)
	}
	if entries[0]["reason"] != "harassing another member in the comments" {
		t.Errorf("reason = %v, want the moderator's words", entries[0]["reason"])
	}
	if expiresAt, _ := entries[0]["expires_at"].(string); expiresAt == "" {
		t.Errorf("expires_at is missing; a suspended member needs to know when it ends: %v", entries[0])
	}
}

// Most members will only ever see this response.
func TestOwnModerationHistoryIsEmptyForAMemberWhoHasNeverBeenModerated(t *testing.T) {
	pool := testsupport.TestDB(t)

	member := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("ownhistclean"), domain.RoleMember, 80.0)

	status, entries, raw := readOwnHistory(t, testServer(t, pool, member).Handler())
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", status, raw)
	}
	if len(entries) != 0 {
		t.Errorf("%d entries, want 0; body: %s", len(entries), raw)
	}
	if !strings.Contains(raw, `"actions":[]`) {
		t.Errorf("body = %s, want an empty array rather than null", raw)
	}
}

// The subject comes from the session and appears nowhere in the request, so
// there is no id to tamper with — but the property worth pinning is the one a
// reader cares about: one member's history never contains another's.
func TestOwnModerationHistoryShowsOnlyTheCallersOwnActions(t *testing.T) {
	pool := testsupport.TestDB(t)

	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("ownhistsplitmod"), domain.RoleModerator, 90.0)
	warned := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("ownhistwarned"), domain.RoleMember, 80.0)
	bystander := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("ownhistbystander"), domain.RoleMember, 80.0)

	takeAction(t, testServer(t, pool, moderator).Handler(), map[string]any{
		"target_user_id": warned.ID,
		"action_type":    "warn",
		"severity":       2,
		"reason":         "an argument that got personal",
	})

	_, warnedEntries, _ := readOwnHistory(t, testServer(t, pool, freshUser(t, pool, warned.ID)).Handler())
	if len(warnedEntries) != 1 {
		t.Errorf("the warned member sees %d entries, want 1", len(warnedEntries))
	}

	_, bystanderEntries, raw := readOwnHistory(t, testServer(t, pool, bystander).Handler())
	if len(bystanderEntries) != 0 {
		t.Errorf("a bystander sees %d entries, want 0; body: %s", len(bystanderEntries), raw)
	}
	if strings.Contains(raw, "an argument that got personal") {
		t.Errorf("another member's reason reached a bystander: %s", raw)
	}
}

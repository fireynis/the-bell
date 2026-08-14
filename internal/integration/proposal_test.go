//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/testsupport"
)

// The council's motions are the one place in the application where a vote
// changes somebody's role, so these tests drive the whole stack — real HTTP
// routes, real services, real Postgres — from raising a motion to the row in
// users that the passing vote rewrote.
//
// The system this replaces recorded votes and did nothing with them. Every test
// below therefore ends by checking a fact outside the proposal: a role, a
// role_history row, a config value.

type proposalResponse struct {
	ID                   string  `json:"id"`
	Type                 string  `json:"type"`
	TargetUserID         string  `json:"target_user_id"`
	TargetDisplayName    string  `json:"target_display_name"`
	Rationale            string  `json:"rationale"`
	CreatedBy            string  `json:"created_by"`
	CreatedByDisplayName string  `json:"created_by_display_name"`
	Status               string  `json:"status"`
	DecidedAt            string  `json:"decided_at"`
	ApproveCount         int64   `json:"approve_count"`
	RejectCount          int64   `json:"reject_count"`
	CouncilSize          int64   `json:"council_size"`
	MyVote               *string `json:"my_vote"`
}

// createProposal raises a motion as the server's authenticated caller.
func createProposal(t *testing.T, h http.Handler, ptype, targetID, rationale string) *httptest.ResponseRecorder {
	t.Helper()

	payload := map[string]any{"type": ptype, "rationale": rationale}
	if targetID != "" {
		payload["target_user_id"] = targetID
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/proposals", bytes.NewReader(mustJSON(t, payload)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// mustCreateProposal raises a motion and returns it, failing on anything but a
// 201.
func mustCreateProposal(t *testing.T, h http.Handler, ptype, targetID, rationale string) proposalResponse {
	t.Helper()

	rec := createProposal(t, h, ptype, targetID, rationale)
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating %s proposal: status %d: %s", ptype, rec.Code, rec.Body.String())
	}
	return decodeProposal(t, rec)
}

func voteOnProposal(t *testing.T, h http.Handler, proposalID string, approve bool) *httptest.ResponseRecorder {
	t.Helper()

	body := mustJSON(t, map[string]any{"approve": approve})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/proposals/"+proposalID+"/votes", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func mustVote(t *testing.T, h http.Handler, proposalID string, approve bool) proposalResponse {
	t.Helper()

	rec := voteOnProposal(t, h, proposalID, approve)
	if rec.Code != http.StatusOK {
		t.Fatalf("voting on %s: status %d: %s", proposalID, rec.Code, rec.Body.String())
	}
	return decodeProposal(t, rec)
}

func decodeProposal(t *testing.T, rec *httptest.ResponseRecorder) proposalResponse {
	t.Helper()

	var p proposalResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decoding proposal: %v; body: %s", err, rec.Body.String())
	}
	return p
}

func listProposals(t *testing.T, h http.Handler, query string) []proposalResponse {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/proposals"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("listing proposals: status %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Proposals []proposalResponse `json:"proposals"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding listing: %v; body: %s", err, rec.Body.String())
	}
	return body.Proposals
}

// roleOf reads the column a passing motion has to have rewritten.
func roleOf(t *testing.T, pool *pgxpool.Pool, userID string) domain.Role {
	t.Helper()

	var role string
	if err := pool.QueryRow(context.Background(),
		`SELECT role FROM users WHERE id = $1`, userID).Scan(&role); err != nil {
		t.Fatalf("reading role: %v", err)
	}
	return domain.Role(role)
}

func roleHistoryFor(t *testing.T, pool *pgxpool.Pool, userID string) []struct{ Old, New, Reason string } {
	t.Helper()

	rows, err := pool.Query(context.Background(),
		`SELECT old_role, new_role, reason FROM role_history WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		t.Fatalf("reading role history: %v", err)
	}
	defer rows.Close()

	var out []struct{ Old, New, Reason string }
	for rows.Next() {
		var entry struct{ Old, New, Reason string }
		if err := rows.Scan(&entry.Old, &entry.New, &entry.Reason); err != nil {
			t.Fatalf("scanning role history: %v", err)
		}
		out = append(out, entry)
	}
	return out
}

// setDisplayName gives a fixture user a name, so a test can tell a join that
// worked from one that returned the empty string either way.
func setDisplayName(t *testing.T, pool *pgxpool.Pool, userID, name string) {
	t.Helper()

	if _, err := pool.Exec(context.Background(),
		`UPDATE users SET display_name = $2 WHERE id = $1`, userID, name); err != nil {
		t.Fatalf("setting display name: %v", err)
	}
}

func townConfig(t *testing.T, pool *pgxpool.Pool, key string) string {
	t.Helper()

	var value string
	if err := pool.QueryRow(context.Background(),
		`SELECT value FROM town_config WHERE key = $1`, key).Scan(&value); err != nil {
		t.Fatalf("reading town config %q: %v", key, err)
	}
	return value
}

// A promotion carried by the council must actually seat the moderator, and the
// change must be recorded the way every other role change in the application is.
func TestProposals_PassingPromotionSeatsTheModerator(t *testing.T) {
	pool := testsupport.TestDB(t)

	councilA := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("prop-council-a"), domain.RoleCouncil, 100)
	councilB := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("prop-council-b"), domain.RoleCouncil, 100)
	testsupport.TestUser(t, pool, testsupport.UniqueKratosID("prop-council-c"), domain.RoleCouncil, 100)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("prop-mod"), domain.RoleModerator, 85)

	serverA := testServer(t, pool, councilA).Handler()
	serverB := testServer(t, pool, councilB).Handler()

	created := mustCreateProposal(t, serverA, "council_promotion", target.ID, "she has run the report queue for a year")
	if created.Status != "open" || created.CouncilSize != 3 {
		t.Fatalf("created = %+v, want an open motion before a council of 3", created)
	}
	if created.TargetUserID != target.ID {
		t.Errorf("target = %q, want %q", created.TargetUserID, target.ID)
	}

	first := mustVote(t, serverA, created.ID, true)
	if first.Status != "open" || first.ApproveCount != 1 {
		t.Fatalf("after one of three: %+v, want it still open", first)
	}
	if first.MyVote == nil || *first.MyVote != "approve" {
		t.Errorf("my_vote = %v, want approve", first.MyVote)
	}
	if got := roleOf(t, pool, target.ID); got != domain.RoleModerator {
		t.Fatalf("role = %q before the majority, want moderator", got)
	}

	second := mustVote(t, serverB, created.ID, true)
	if second.Status != "passed" {
		t.Fatalf("after two of three: status %q, want passed; %+v", second.Status, second)
	}
	if second.DecidedAt == "" {
		t.Error("a decided motion came back without decided_at")
	}

	// The point of the whole feature: the vote changed something.
	if got := roleOf(t, pool, target.ID); got != domain.RoleCouncil {
		t.Fatalf("role = %q after the motion carried, want council", got)
	}

	history := roleHistoryFor(t, pool, target.ID)
	if len(history) != 1 {
		t.Fatalf("%d role_history rows, want 1", len(history))
	}
	if history[0].Old != "moderator" || history[0].New != "council" {
		t.Errorf("role_history = %+v, want moderator->council", history[0])
	}
	if history[0].Reason == "" {
		t.Error("the role change was recorded with no reason")
	}
}

// A removal is decided by the council minus the person being removed, and the
// target's own vote is refused.
func TestProposals_RemovalExcludesTheTargetAndReturnsThemToMembership(t *testing.T) {
	pool := testsupport.TestDB(t)

	councilA := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rem-council-a"), domain.RoleCouncil, 100)
	councilB := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rem-council-b"), domain.RoleCouncil, 100)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rem-target"), domain.RoleCouncil, 100)

	serverA := testServer(t, pool, councilA).Handler()
	serverB := testServer(t, pool, councilB).Handler()
	serverTarget := testServer(t, pool, target).Handler()

	created := mustCreateProposal(t, serverA, "council_removal", target.ID, "they have not attended in six months")
	// Three on the council, one of them the target: two may vote.
	if created.CouncilSize != 2 {
		t.Fatalf("council_size = %d, want the council minus the target (2)", created.CouncilSize)
	}

	// The target cannot vote on their own removal.
	rec := voteOnProposal(t, serverTarget, created.ID, false)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("the target's vote returned %d, want 403: %s", rec.Code, rec.Body.String())
	}

	mustVote(t, serverA, created.ID, true)
	final := mustVote(t, serverB, created.ID, true)

	if final.Status != "passed" {
		t.Fatalf("status = %q, want passed; %+v", final.Status, final)
	}
	if got := roleOf(t, pool, target.ID); got != domain.RoleMember {
		t.Fatalf("role = %q, want member", got)
	}

	history := roleHistoryFor(t, pool, target.ID)
	if len(history) != 1 || history[0].Old != "council" || history[0].New != "member" {
		t.Errorf("role_history = %+v, want one council->member row", history)
	}
}

// Bootstrap re-entry is the reversal that was missing: leaving bootstrap mode
// was a one-way door, so a town that grew past the threshold and then shrank
// had no way back to the only mechanism that admits residents without a vouch.
func TestProposals_BootstrapReentryTurnsTheModeBackOn(t *testing.T) {
	pool := testsupport.TestDB(t)

	councilA := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("boot-council-a"), domain.RoleCouncil, 100)
	councilB := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("boot-council-b"), domain.RoleCouncil, 100)

	if townConfig(t, pool, "bootstrap_mode") != "false" {
		t.Fatal("the test town did not start out of bootstrap mode")
	}

	serverA := testServer(t, pool, councilA).Handler()
	serverB := testServer(t, pool, councilB).Handler()

	created := mustCreateProposal(t, serverA, "bootstrap_reentry", "", "most of the town moved away after the mill closed")
	if created.TargetUserID != "" {
		t.Errorf("a town-wide motion carried a target %q", created.TargetUserID)
	}

	mustVote(t, serverA, created.ID, true)
	final := mustVote(t, serverB, created.ID, true)

	if final.Status != "passed" {
		t.Fatalf("status = %q, want passed; %+v", final.Status, final)
	}
	if got := townConfig(t, pool, "bootstrap_mode"); got != "true" {
		t.Fatalf("bootstrap_mode = %q, want true", got)
	}
}

// The precondition with teeth: above the exit threshold the mode would be
// switched straight back off by the next approval, so the motion is refused
// rather than allowed to pass and evaporate.
func TestProposals_BootstrapReentryRefusedInATownAboveTheThreshold(t *testing.T) {
	pool := testsupport.TestDB(t)

	council := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("big-council"), domain.RoleCouncil, 100)
	// 20 active members ends bootstrap mode; the council member counts too.
	for i := 0; i < 20; i++ {
		testsupport.TestUser(t, pool, testsupport.UniqueKratosID("big-member"), domain.RoleMember, 50)
	}

	h := testServer(t, pool, council).Handler()

	rec := createProposal(t, h, "bootstrap_reentry", "", "let us go back")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	if got := townConfig(t, pool, "bootstrap_mode"); got != "false" {
		t.Errorf("bootstrap_mode = %q, want it untouched", got)
	}
}

// Rejection is a decision too, and it must leave the target exactly as it found
// them.
func TestProposals_RejectedPromotionChangesNothing(t *testing.T) {
	pool := testsupport.TestDB(t)

	councilA := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rej-council-a"), domain.RoleCouncil, 100)
	councilB := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rej-council-b"), domain.RoleCouncil, 100)
	testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rej-council-c"), domain.RoleCouncil, 100)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("rej-mod"), domain.RoleModerator, 85)

	serverA := testServer(t, pool, councilA).Handler()
	serverB := testServer(t, pool, councilB).Handler()

	created := mustCreateProposal(t, serverA, "council_promotion", target.ID, "I am not convinced")
	mustVote(t, serverA, created.ID, false)
	final := mustVote(t, serverB, created.ID, false)

	if final.Status != "rejected" {
		t.Fatalf("status = %q, want rejected", final.Status)
	}
	if got := roleOf(t, pool, target.ID); got != domain.RoleModerator {
		t.Errorf("role = %q, want the unchanged moderator", got)
	}
	if history := roleHistoryFor(t, pool, target.ID); len(history) != 0 {
		t.Errorf("a rejected motion wrote role history: %+v", history)
	}
}

// One vote each, enforced by the schema's unique index as well as by the
// service, so a double-click is a 400 rather than a second ballot.
func TestProposals_VotingTwiceIsRefused(t *testing.T) {
	pool := testsupport.TestDB(t)

	council := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("twice-council"), domain.RoleCouncil, 100)
	testsupport.TestUser(t, pool, testsupport.UniqueKratosID("twice-council-b"), domain.RoleCouncil, 100)
	testsupport.TestUser(t, pool, testsupport.UniqueKratosID("twice-council-c"), domain.RoleCouncil, 100)

	h := testServer(t, pool, council).Handler()

	created := mustCreateProposal(t, h, "bootstrap_reentry", "", "the town has shrunk")
	mustVote(t, h, created.ID, true)

	rec := voteOnProposal(t, h, created.ID, false)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("second vote returned %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// Only the council may see or act on any of this. A moderator is the closest
// role that is not on the council, which makes it the one worth checking.
func TestProposals_ModeratorIsRefusedEverywhere(t *testing.T) {
	pool := testsupport.TestDB(t)

	council := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("guard-council"), domain.RoleCouncil, 100)
	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("guard-mod"), domain.RoleModerator, 85)

	councilServer := testServer(t, pool, council).Handler()
	modServer := testServer(t, pool, moderator).Handler()

	created := mustCreateProposal(t, councilServer, "bootstrap_reentry", "", "the town has shrunk")

	tests := []struct {
		name string
		req  *http.Request
	}{
		{"list", httptest.NewRequest(http.MethodGet, "/api/v1/admin/proposals", nil)},
		{"create", httptest.NewRequest(http.MethodPost, "/api/v1/admin/proposals",
			bytes.NewReader(mustJSON(t, map[string]any{"type": "bootstrap_reentry", "rationale": "let me in"})))},
		{"vote", httptest.NewRequest(http.MethodPost, "/api/v1/admin/proposals/"+created.ID+"/votes",
			bytes.NewReader(mustJSON(t, map[string]any{"approve": true})))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			modServer.ServeHTTP(rec, tt.req)

			if rec.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403; body: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// The listing is per caller: the same motion shows one council member their
// approve and another nothing at all.
func TestProposals_ListingIsPerCallerAndSeparatesDecided(t *testing.T) {
	pool := testsupport.TestDB(t)

	councilA := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("list-council-a"), domain.RoleCouncil, 100)
	councilB := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("list-council-b"), domain.RoleCouncil, 100)
	testsupport.TestUser(t, pool, testsupport.UniqueKratosID("list-council-c"), domain.RoleCouncil, 100)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("list-mod"), domain.RoleModerator, 85)

	// testsupport.TestUser deliberately leaves the display name blank, so the
	// listing's joins are only exercised once somebody has a name to join to.
	setDisplayName(t, pool, councilA.ID, "Ada")
	setDisplayName(t, pool, target.ID, "Grace")

	serverA := testServer(t, pool, councilA).Handler()
	serverB := testServer(t, pool, councilB).Handler()

	open := mustCreateProposal(t, serverA, "council_promotion", target.ID, "she has run the report queue for a year")
	mustVote(t, serverA, open.ID, true)

	fromA := listProposals(t, serverA, "?status=open")
	if len(fromA) != 1 {
		t.Fatalf("%d open motions for A, want 1", len(fromA))
	}
	if fromA[0].MyVote == nil || *fromA[0].MyVote != "approve" {
		t.Errorf("A's my_vote = %v, want approve", fromA[0].MyVote)
	}
	if fromA[0].TargetDisplayName != "Grace" || fromA[0].CreatedByDisplayName != "Ada" {
		t.Errorf("names = %q/%q, want Grace/Ada — the listing's joins are what supply them",
			fromA[0].TargetDisplayName, fromA[0].CreatedByDisplayName)
	}

	fromB := listProposals(t, serverB, "?status=open")
	if len(fromB) != 1 {
		t.Fatalf("%d open motions for B, want 1", len(fromB))
	}
	if fromB[0].MyVote != nil {
		t.Errorf("B's my_vote = %q, want null — B has not voted", *fromB[0].MyVote)
	}
	if fromB[0].ApproveCount != 1 {
		t.Errorf("B sees %d approvals, want 1 — the tally is shared even though the vote is not", fromB[0].ApproveCount)
	}

	// Carry it, and it moves from one listing to the other.
	mustVote(t, serverB, open.ID, true)

	if remaining := listProposals(t, serverA, "?status=open"); len(remaining) != 0 {
		t.Errorf("%d open motions after the decision, want none", len(remaining))
	}
	decided := listProposals(t, serverA, "?status=decided")
	if len(decided) != 1 || decided[0].ID != open.ID {
		t.Fatalf("decided listing = %+v, want the settled motion", decided)
	}
	if decided[0].Status != "passed" || decided[0].DecidedAt == "" {
		t.Errorf("decided motion = %+v, want a passed motion with a decision time", decided[0])
	}
}

// A promotion whose target is no longer a moderator when the vote finishes is
// recorded as rejected rather than passed, because nothing happened — and a
// "passed" motion that changed nothing would be a lie in the council's record.
func TestProposals_PromotionOfADemotedTargetIsRejected(t *testing.T) {
	pool := testsupport.TestDB(t)

	councilA := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("stale-council-a"), domain.RoleCouncil, 100)
	councilB := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("stale-council-b"), domain.RoleCouncil, 100)
	testsupport.TestUser(t, pool, testsupport.UniqueKratosID("stale-council-c"), domain.RoleCouncil, 100)
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("stale-mod"), domain.RoleModerator, 85)

	serverA := testServer(t, pool, councilA).Handler()
	serverB := testServer(t, pool, councilB).Handler()

	created := mustCreateProposal(t, serverA, "council_promotion", target.ID, "she has run the report queue for a year")
	mustVote(t, serverA, created.ID, true)

	// The role checker demotes them while the motion is still open.
	if _, err := pool.Exec(context.Background(),
		`UPDATE users SET role = 'member' WHERE id = $1`, target.ID); err != nil {
		t.Fatalf("demoting the target: %v", err)
	}

	final := mustVote(t, serverB, created.ID, true)
	if final.Status != "rejected" {
		t.Fatalf("status = %q, want rejected", final.Status)
	}
	if got := roleOf(t, pool, target.ID); got != domain.RoleMember {
		t.Errorf("role = %q, want the demoted member left alone", got)
	}
	if history := roleHistoryFor(t, pool, target.ID); len(history) != 0 {
		t.Errorf("an unexecutable motion wrote role history: %+v", history)
	}
}

// The old shell endpoints are gone, not deprecated. Nothing but the previous
// admin screen ever called them and nothing they recorded was ever acted on.
func TestProposals_OldCouncilVotesEndpointsAreGone(t *testing.T) {
	pool := testsupport.TestDB(t)

	council := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("gone-council"), domain.RoleCouncil, 100)
	h := testServer(t, pool, council).Handler()

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/api/v1/admin/council/votes", bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Content-Type", "application/json")
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("%s /api/v1/admin/council/votes = %d, want 404", method, rec.Code)
		}
	}
}

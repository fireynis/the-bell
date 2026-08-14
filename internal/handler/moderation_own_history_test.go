package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/handler"
)

// GET /api/v1/users/me/moderation-history is the member's side of the audit
// trail. What it must not carry matters as much as what it does, so these
// assert over the response bytes as well as over the decoded shape.

// ownHistoryMember is the signed-in caller these tests speak as. Nothing in the
// request names them: the handler takes the subject from the session, which is
// the whole of the authorization.
func ownHistoryMember() *domain.User {
	return &domain.User{ID: "member-1", Role: domain.RoleMember, IsActive: true}
}

// ownHistoryEntry decodes one entry loosely, so a field the handler is supposed
// to have stripped shows up as an unexpected key rather than being silently
// discarded into a typed struct that has no room for it.
type ownHistoryBody struct {
	Actions []map[string]any `json:"actions"`
}

func ownHistoryHandler(t *testing.T, actions []*domain.ModerationAction, penalties map[string][]domain.TrustPenalty) *handler.ModerationHandler {
	t.Helper()

	repo := newMockActionRepoH()
	repo.actionsByTarget = actions

	lister := newMockPenaltyListerH()
	for actionID, list := range penalties {
		lister.penalties[actionID] = list
	}

	return handler.NewModerationHandler(newTestModerationActionServiceWithPenalties(
		repo, newMockActionUserLookup(), newMockPenaltyRepoH(), newMockPenaltyGraphH(), lister,
	))
}

func getOwnHistory(h *handler.ModerationHandler, user *domain.User, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me/moderation-history"+query, nil)
	if user != nil {
		req = withUser(req, user)
	}
	rec := httptest.NewRecorder()
	h.OwnHistory(rec, req)
	return rec
}

func TestModerationHandler_OwnHistory_TellsTheMemberWhatHappenedAndWhy(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	decays := now.AddDate(0, 0, 90)

	h := ownHistoryHandler(t,
		[]*domain.ModerationAction{{
			ID: "act-1", TargetUserID: "member-1", ModeratorID: "mod-1",
			ModeratorDisplayName: "Mallory",
			Action:               domain.ActionWarn, Severity: 1,
			Reason: "posting the same thing repeatedly", CreatedAt: now,
		}},
		map[string][]domain.TrustPenalty{
			"act-1": {{ID: "pen-1", UserID: "member-1", ModerationActionID: "act-1", PenaltyAmount: 5, HopDepth: 0, CreatedAt: now, DecaysAt: &decays}},
		},
	)

	rec := getOwnHistory(h, ownHistoryMember(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var body ownHistoryBody
	decodeBody(t, rec, &body)
	if len(body.Actions) != 1 {
		t.Fatalf("%d actions, want 1", len(body.Actions))
	}

	entry := body.Actions[0]
	if entry["action"] != "warn" {
		t.Errorf("action = %v, want warn", entry["action"])
	}
	if entry["reason"] != "posting the same thing repeatedly" {
		t.Errorf("reason = %v, want the moderator's words verbatim", entry["reason"])
	}
	if entry["created_at"] != "2026-03-01T12:00:00Z" {
		t.Errorf("created_at = %v, want 2026-03-01T12:00:00Z", entry["created_at"])
	}

	penalty, ok := entry["penalty"].(map[string]any)
	if !ok {
		t.Fatalf("penalty = %v, want an object", entry["penalty"])
	}
	if penalty["amount"] != 5.0 {
		t.Errorf("penalty.amount = %v, want 5", penalty["amount"])
	}
	if penalty["decays_at"] != "2026-05-30T12:00:00Z" {
		t.Errorf("penalty.decays_at = %v, want 2026-05-30T12:00:00Z", penalty["decays_at"])
	}
}

// The response must not name the moderator, in any field, at any depth. This is
// the policy line the endpoint exists to hold, so it is asserted on the raw
// bytes rather than on the keys a decoder happened to keep.
func TestModerationHandler_OwnHistory_NamesNoModerator(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	h := ownHistoryHandler(t,
		[]*domain.ModerationAction{{
			ID: "act-1", TargetUserID: "member-1", ModeratorID: "mod-1",
			ModeratorDisplayName: "Mallory", TargetDisplayName: "Ada",
			Action: domain.ActionSuspend, Severity: 4,
			Reason: "harassment", CreatedAt: now,
		}},
		nil,
	)

	rec := getOwnHistory(h, ownHistoryMember(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	for _, forbidden := range []string{"mod-1", "Mallory", "moderator"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("response contains %q: %s", forbidden, body)
		}
	}
}

// What an action cost the people who vouched for the member is their business,
// and putting it here would show a member exactly who stands one hop from them
// in the vouch graph.
func TestModerationHandler_OwnHistory_ExcludesPenaltiesPropagatedToOthers(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	h := ownHistoryHandler(t,
		[]*domain.ModerationAction{{
			ID: "act-1", TargetUserID: "member-1", ModeratorID: "mod-1",
			Action: domain.ActionMute, Severity: 3, Reason: "spam", CreatedAt: now,
		}},
		map[string][]domain.TrustPenalty{
			"act-1": {
				{ID: "pen-1", UserID: "member-1", ModerationActionID: "act-1", PenaltyAmount: 25, HopDepth: 0, CreatedAt: now},
				{ID: "pen-2", UserID: "voucher-1", ModerationActionID: "act-1", PenaltyAmount: 15, HopDepth: 1, CreatedAt: now},
			},
		},
	)

	rec := getOwnHistory(h, ownHistoryMember(), "")
	body := rec.Body.String()
	if !strings.Contains(body, "25") {
		t.Errorf("response omits the member's own penalty: %s", body)
	}
	if strings.Contains(body, "voucher-1") || strings.Contains(body, "15") {
		t.Errorf("response carries a penalty belonging to somebody else: %s", body)
	}
}

// A ban's penalty never decays. The member is told that by `penalty` being
// present with no `decays_at`; an action whose penalty was never recorded has
// no `penalty` key at all, so the two cannot be read as the same thing.
func TestModerationHandler_OwnHistory_OmitsDecaysAtForAPermanentPenalty(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	h := ownHistoryHandler(t,
		[]*domain.ModerationAction{{
			ID: "act-ban", TargetUserID: "member-1", ModeratorID: "mod-1",
			Action: domain.ActionBan, Severity: 5, Reason: "repeated harassment", CreatedAt: now,
		}},
		map[string][]domain.TrustPenalty{
			"act-ban": {{ID: "pen-1", UserID: "member-1", ModerationActionID: "act-ban", PenaltyAmount: 100, HopDepth: 0, CreatedAt: now, DecaysAt: nil}},
		},
	)

	rec := getOwnHistory(h, ownHistoryMember(), "")

	var body ownHistoryBody
	decodeBody(t, rec, &body)
	penalty, ok := body.Actions[0]["penalty"].(map[string]any)
	if !ok {
		t.Fatalf("penalty = %v, want an object saying the 100 points are permanent", body.Actions[0]["penalty"])
	}
	if _, present := penalty["decays_at"]; present {
		t.Errorf("penalty.decays_at is present for a ban: %v", penalty)
	}
	if penalty["amount"] != 100.0 {
		t.Errorf("penalty.amount = %v, want 100", penalty["amount"])
	}
}

func TestModerationHandler_OwnHistory_CarriesTheRestrictionExpiry(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	expires := now.Add(72 * time.Hour)

	h := ownHistoryHandler(t,
		[]*domain.ModerationAction{{
			ID: "act-1", TargetUserID: "member-1", ModeratorID: "mod-1",
			Action: domain.ActionMute, Severity: 3, Reason: "spam",
			CreatedAt: now, ExpiresAt: &expires,
		}},
		nil,
	)

	rec := getOwnHistory(h, ownHistoryMember(), "")

	var body ownHistoryBody
	decodeBody(t, rec, &body)
	if body.Actions[0]["expires_at"] != "2026-03-04T12:00:00Z" {
		t.Errorf("expires_at = %v, want 2026-03-04T12:00:00Z", body.Actions[0]["expires_at"])
	}
}

// A clean record is 200 with an empty array, not 404 and not `null`. Most
// members will only ever see this response, and it is the one the reassuring
// empty state is rendered from.
func TestModerationHandler_OwnHistory_CleanRecordIsAnEmptyArray(t *testing.T) {
	h := ownHistoryHandler(t, nil, nil)

	rec := getOwnHistory(h, ownHistoryMember(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"actions":[]}` {
		t.Errorf("body = %s, want {\"actions\":[]}", got)
	}
}

// A suspended member is exactly who most needs to read this, so the handler
// itself must not turn them away. RequireActive is skipped at the route; if the
// handler grew its own active check the route's exception would be undone
// without the route changing.
func TestModerationHandler_OwnHistory_ServesASuspendedMember(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	suspended := &domain.User{ID: "member-1", Role: domain.RoleMember, IsActive: false}

	h := ownHistoryHandler(t,
		[]*domain.ModerationAction{{
			ID: "act-1", TargetUserID: "member-1", ModeratorID: "mod-1",
			Action: domain.ActionSuspend, Severity: 4, Reason: "harassment", CreatedAt: now,
		}},
		nil,
	)

	rec := getOwnHistory(h, suspended, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; a suspended member must be able to read why", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "harassment") {
		t.Errorf("the reason did not reach the suspended member: %s", rec.Body.String())
	}
}

// A banned member likewise. The route lets them through; nothing here may.
func TestModerationHandler_OwnHistory_ServesABannedMember(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	banned := &domain.User{ID: "member-1", Role: domain.RoleBanned, IsActive: true}

	h := ownHistoryHandler(t,
		[]*domain.ModerationAction{{
			ID: "act-1", TargetUserID: "member-1", ModeratorID: "mod-1",
			Action: domain.ActionBan, Severity: 5, Reason: "repeated harassment", CreatedAt: now,
		}},
		nil,
	)

	rec := getOwnHistory(h, banned, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; a banned member must be able to read why", rec.Code)
	}
}

func TestModerationHandler_OwnHistory_NoUserIsUnauthorized(t *testing.T) {
	h := ownHistoryHandler(t, nil, nil)

	rec := getOwnHistory(h, nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// Offset pagination, with the same defaults and ceiling as every other listing.
// A query string the handler mis-parses is invisible on a short history.
func TestModerationHandler_OwnHistory_ParsesPagination(t *testing.T) {
	repo := newMockActionRepoH()
	h := handler.NewModerationHandler(newTestModerationActionServiceWithPenalties(
		repo, newMockActionUserLookup(), newMockPenaltyRepoH(), newMockPenaltyGraphH(), newMockPenaltyListerH(),
	))

	for _, tc := range []struct{ name, query string }{
		{"defaults", ""},
		{"an explicit page", "?limit=5&offset=10"},
		{"a limit above the ceiling", "?limit=5000"},
		{"a negative offset", "?offset=-3"},
		{"an unparseable limit", "?limit=lots"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := getOwnHistory(h, ownHistoryMember(), tc.query)
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

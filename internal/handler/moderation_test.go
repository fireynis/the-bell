package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/handler"
	"github.com/fireynis/the-bell/internal/service"
)

// --- mock ModerationActionRepository ---

type mockActionRepo struct {
	actions            []*domain.ModerationAction
	actionsByTarget    []*domain.ModerationAction
	actionsByModerator []*domain.ModerationAction
	listErr            error
}

func newMockActionRepoH() *mockActionRepo {
	return &mockActionRepo{}
}

func (m *mockActionRepo) CreateModerationAction(_ context.Context, action *domain.ModerationAction) error {
	m.actions = append(m.actions, action)
	return nil
}

func (m *mockActionRepo) ListActionsByTarget(_ context.Context, _ string, _, _ int) ([]*domain.ModerationAction, error) {
	return m.actionsByTarget, m.listErr
}

func (m *mockActionRepo) ListActionsByModerator(_ context.Context, _ string, _, _ int) ([]*domain.ModerationAction, error) {
	return m.actionsByModerator, nil
}

// --- mock ActionUserLookup ---

type mockActionUserLookup struct {
	users map[string]*domain.User
}

func newMockActionUserLookup() *mockActionUserLookup {
	return &mockActionUserLookup{users: make(map[string]*domain.User)}
}

func (m *mockActionUserLookup) GetUserByID(_ context.Context, id string) (*domain.User, error) {
	u, ok := m.users[id]
	if !ok {
		return nil, service.ErrNotFound
	}
	return u, nil
}

// --- mock PenaltyRepository ---

type mockPenaltyRepo struct {
	penalties []*domain.TrustPenalty
}

func newMockPenaltyRepoH() *mockPenaltyRepo {
	return &mockPenaltyRepo{}
}

func (m *mockPenaltyRepo) CreateTrustPenalty(_ context.Context, p *domain.TrustPenalty) error {
	m.penalties = append(m.penalties, p)
	return nil
}

// --- mock PenaltyLister ---

type mockPenaltyListerH struct {
	penalties map[string][]domain.TrustPenalty
}

func newMockPenaltyListerH() *mockPenaltyListerH {
	return &mockPenaltyListerH{penalties: make(map[string][]domain.TrustPenalty)}
}

func (m *mockPenaltyListerH) CreateTrustPenalty(_ context.Context, p *domain.TrustPenalty) error {
	return nil
}

// Returned flat and in the order asked for, as the batched query does; grouping
// them back onto their actions is the service's job, not the store's.
func (m *mockPenaltyListerH) ListPenaltiesByActionIDs(_ context.Context, actionIDs []string) ([]domain.TrustPenalty, error) {
	var out []domain.TrustPenalty
	for _, id := range actionIDs {
		out = append(out, m.penalties[id]...)
	}
	return out, nil
}

// --- mock PenaltyGraphQuerier ---

type mockPenaltyGraph struct {
	vouchers map[string]int
	err      error
}

func newMockPenaltyGraphH() *mockPenaltyGraph {
	return &mockPenaltyGraph{vouchers: make(map[string]int)}
}

func (m *mockPenaltyGraph) FindVouchersWithDepth(_ context.Context, _ string, _ int) (map[string]int, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.vouchers, nil
}

// --- test helpers ---

// mockReliefRepoH is the moderation_reliefs store these handler tests run
// against. LiftMute records the release here, so a nil one would make every
// lift fail at the HTTP boundary for a reason unrelated to what is under test.
type mockReliefRepoH struct {
	reliefs []*domain.ModerationRelief
}

func newMockReliefRepoH() *mockReliefRepoH { return &mockReliefRepoH{} }

func (m *mockReliefRepoH) CreateModerationRelief(_ context.Context, relief *domain.ModerationRelief) error {
	m.reliefs = append(m.reliefs, relief)
	return nil
}

func (m *mockReliefRepoH) ListLiftsInForce(_ context.Context, targetUserID string, reliefType domain.ReliefType, limit int) ([]domain.ModerationRelief, error) {
	var out []domain.ModerationRelief
	for _, r := range m.reliefs {
		if r.TargetUserID != targetUserID || r.Type != reliefType || !r.WasInForce || len(out) == limit {
			continue
		}
		out = append(out, *r)
	}
	return out, nil
}

func newTestModerationActionService(
	actions service.ModerationActionRepository,
	users service.ActionUserLookup,
	penalties service.PenaltyRepository,
	graph service.PenaltyGraphQuerier,
) *service.ModerationActionService {
	modSvc := service.NewModerationService(penalties, graph, func() time.Time { return fixedNow })
	return service.NewModerationActionService(actions, users, modSvc, nil, nil, newMockReliefRepoH(), func() time.Time { return fixedNow })
}

func newTestModerationActionServiceWithPenalties(
	actions service.ModerationActionRepository,
	users service.ActionUserLookup,
	penalties service.PenaltyRepository,
	graph service.PenaltyGraphQuerier,
	penaltyLister service.PenaltyLister,
) *service.ModerationActionService {
	modSvc := service.NewModerationService(penalties, graph, func() time.Time { return fixedNow })
	return service.NewModerationActionService(actions, users, modSvc, nil, penaltyLister, newMockReliefRepoH(), func() time.Time { return fixedNow })
}

// --- TakeAction success ---

func TestModerationHandler_TakeAction(t *testing.T) {
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true}

	svc := newTestModerationActionService(
		newMockActionRepoH(), users,
		newMockPenaltyRepoH(), newMockPenaltyGraphH(),
	)
	h := handler.NewModerationHandler(svc)

	body := `{"target_user_id":"target-1","action_type":"warn","severity":1,"reason":"first warning"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/moderation/actions", strings.NewReader(body))
	req = withUser(req, testModerator())
	rec := httptest.NewRecorder()

	h.TakeAction(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var result service.TakeActionResult
	decodeBody(t, rec, &result)
	if result.Action == nil {
		t.Fatal("expected action in response")
	}
	if result.Action.ID == "" {
		t.Error("expected non-empty action ID")
	}
	if result.Action.TargetUserID != "target-1" {
		t.Errorf("target = %q, want %q", result.Action.TargetUserID, "target-1")
	}
}

// --- TakeAction: the ban floor at the HTTP boundary ---

// The route group requires only the moderator role, so a moderator reaches this
// handler and the refusal has to arrive as a 403 rather than as a created
// action. The rule itself is the service's, and is covered there; this is about
// the status and the empty audit trail the caller sees.
func TestModerationHandler_TakeAction_BanByAModeratorIsForbidden(t *testing.T) {
	actions := newMockActionRepoH()
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, Role: domain.RoleMember}
	users.users["mod-1"] = testModerator()

	h := handler.NewModerationHandler(newTestModerationActionService(
		actions, users, newMockPenaltyRepoH(), newMockPenaltyGraphH()))

	body := `{"target_user_id":"target-1","action_type":"ban","severity":5,"reason":"spam"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/moderation/actions", strings.NewReader(body))
	req = withUser(req, testModerator())
	rec := httptest.NewRecorder()

	h.TakeAction(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if len(actions.actions) != 0 {
		t.Errorf("%d actions written for a refused ban", len(actions.actions))
	}
}

func TestModerationHandler_TakeAction_BanByTheCouncilIsCreated(t *testing.T) {
	council := &domain.User{ID: "council-1", Role: domain.RoleCouncil, IsActive: true}
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, Role: domain.RoleMember}
	users.users[council.ID] = council

	h := handler.NewModerationHandler(newTestModerationActionService(
		newMockActionRepoH(), users, newMockPenaltyRepoH(), newMockPenaltyGraphH()))

	body := `{"target_user_id":"target-1","action_type":"ban","severity":5,"reason":"spam"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/moderation/actions", strings.NewReader(body))
	req = withUser(req, council)
	rec := httptest.NewRecorder()

	h.TakeAction(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

// --- TakeAction no user ---

func TestModerationHandler_TakeAction_NoUser(t *testing.T) {
	svc := newTestModerationActionService(
		newMockActionRepoH(), newMockActionUserLookup(),
		newMockPenaltyRepoH(), newMockPenaltyGraphH(),
	)
	h := handler.NewModerationHandler(svc)

	body := `{"target_user_id":"target-1","action_type":"warn","severity":1,"reason":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/moderation/actions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.TakeAction(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// --- TakeAction invalid JSON ---

func TestModerationHandler_TakeAction_InvalidJSON(t *testing.T) {
	svc := newTestModerationActionService(
		newMockActionRepoH(), newMockActionUserLookup(),
		newMockPenaltyRepoH(), newMockPenaltyGraphH(),
	)
	h := handler.NewModerationHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/moderation/actions", strings.NewReader(`{bad`))
	req = withUser(req, testModerator())
	rec := httptest.NewRecorder()

	h.TakeAction(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// --- TakeAction validation error ---

func TestModerationHandler_TakeAction_ValidationError(t *testing.T) {
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true}

	svc := newTestModerationActionService(
		newMockActionRepoH(), users,
		newMockPenaltyRepoH(), newMockPenaltyGraphH(),
	)
	h := handler.NewModerationHandler(svc)

	// severity 5 not valid for warn
	body := `{"target_user_id":"target-1","action_type":"warn","severity":5,"reason":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/moderation/actions", strings.NewReader(body))
	req = withUser(req, testModerator())
	rec := httptest.NewRecorder()

	h.TakeAction(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

// --- TakeAction target not found ---

func TestModerationHandler_TakeAction_TargetNotFound(t *testing.T) {
	svc := newTestModerationActionService(
		newMockActionRepoH(), newMockActionUserLookup(),
		newMockPenaltyRepoH(), newMockPenaltyGraphH(),
	)
	h := handler.NewModerationHandler(svc)

	body := `{"target_user_id":"nonexistent","action_type":"warn","severity":1,"reason":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/moderation/actions", strings.NewReader(body))
	req = withUser(req, testModerator())
	rec := httptest.NewRecorder()

	h.TakeAction(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// --- TakeAction with duration ---

func TestModerationHandler_TakeAction_WithDuration(t *testing.T) {
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true}

	svc := newTestModerationActionService(
		newMockActionRepoH(), users,
		newMockPenaltyRepoH(), newMockPenaltyGraphH(),
	)
	h := handler.NewModerationHandler(svc)

	body := `{"target_user_id":"target-1","action_type":"mute","severity":3,"reason":"muted","duration_seconds":3600}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/moderation/actions", strings.NewReader(body))
	req = withUser(req, testModerator())
	rec := httptest.NewRecorder()

	h.TakeAction(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

// --- ListActions: success (by target) ---

func TestModerationHandler_ListActions_ByTarget(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	actionRepo := newMockActionRepoH()
	actionRepo.actionsByTarget = []*domain.ModerationAction{
		{ID: "act-1", TargetUserID: "user-1", ModeratorID: "mod-1", Action: domain.ActionWarn, Severity: 1, Reason: "test", CreatedAt: now},
	}
	penaltyLister := newMockPenaltyListerH()
	penaltyLister.penalties["act-1"] = []domain.TrustPenalty{
		{ID: "pen-1", UserID: "user-1", ModerationActionID: "act-1", PenaltyAmount: 5.0, HopDepth: 0, CreatedAt: now},
	}

	svc := newTestModerationActionServiceWithPenalties(actionRepo, newMockActionUserLookup(), newMockPenaltyRepoH(), newMockPenaltyGraphH(), penaltyLister)
	h := handler.NewModerationHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/moderation/actions/user-1", nil)
	req = withUser(req, testModerator())
	req = withChiURLParam(req, "user_id", "user-1")
	rec := httptest.NewRecorder()

	h.ListActions(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Actions []service.ActionHistoryEntry `json:"actions"`
	}
	decodeBody(t, rec, &resp)
	if len(resp.Actions) != 1 {
		t.Fatalf("got %d actions, want 1", len(resp.Actions))
	}
	if resp.Actions[0].Action.ID != "act-1" {
		t.Errorf("action ID = %q, want %q", resp.Actions[0].Action.ID, "act-1")
	}
	if len(resp.Actions[0].Penalties) != 1 {
		t.Errorf("penalties = %d, want 1", len(resp.Actions[0].Penalties))
	}
}

// --- ListActions: no user in context ---

func TestModerationHandler_ListActions_NoUser(t *testing.T) {
	svc := newTestModerationActionService(
		newMockActionRepoH(), newMockActionUserLookup(),
		newMockPenaltyRepoH(), newMockPenaltyGraphH(),
	)
	h := handler.NewModerationHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/moderation/actions/user-1", nil)
	req = withChiURLParam(req, "user_id", "user-1")
	rec := httptest.NewRecorder()

	h.ListActions(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// --- ListActions: by moderator (council only) ---

func TestModerationHandler_ListActions_ByModerator_Council(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	actionRepo := newMockActionRepoH()
	actionRepo.actionsByModerator = []*domain.ModerationAction{
		{ID: "act-1", TargetUserID: "user-1", ModeratorID: "mod-2", Action: domain.ActionBan, Severity: 5, Reason: "banned", CreatedAt: now},
	}
	penaltyLister := newMockPenaltyListerH()

	svc := newTestModerationActionServiceWithPenalties(actionRepo, newMockActionUserLookup(), newMockPenaltyRepoH(), newMockPenaltyGraphH(), penaltyLister)
	h := handler.NewModerationHandler(svc)

	council := &domain.User{ID: "council-1", Role: domain.RoleCouncil, IsActive: true}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/moderation/actions/mod-2?role=moderator", nil)
	req = withUser(req, council)
	req = withChiURLParam(req, "user_id", "mod-2")
	rec := httptest.NewRecorder()

	h.ListActions(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

// --- ListActions: by moderator (non-council forbidden) ---

func TestModerationHandler_ListActions_ByModerator_NonCouncilForbidden(t *testing.T) {
	svc := newTestModerationActionService(
		newMockActionRepoH(), newMockActionUserLookup(),
		newMockPenaltyRepoH(), newMockPenaltyGraphH(),
	)
	h := handler.NewModerationHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/moderation/actions/mod-2?role=moderator", nil)
	req = withUser(req, testModerator())
	req = withChiURLParam(req, "user_id", "mod-2")
	rec := httptest.NewRecorder()

	h.ListActions(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

// --- ListActions: with pagination ---

func TestModerationHandler_ListActions_Pagination(t *testing.T) {
	actionRepo := newMockActionRepoH()
	penaltyLister := newMockPenaltyListerH()

	svc := newTestModerationActionServiceWithPenalties(actionRepo, newMockActionUserLookup(), newMockPenaltyRepoH(), newMockPenaltyGraphH(), penaltyLister)
	h := handler.NewModerationHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/moderation/actions/user-1?limit=10&offset=5", nil)
	req = withUser(req, testModerator())
	req = withChiURLParam(req, "user_id", "user-1")
	rec := httptest.NewRecorder()

	h.ListActions(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

// --- ListActions: service failure ---

func TestModerationHandler_ListActions_ServiceError(t *testing.T) {
	actionRepo := newMockActionRepoH()
	actionRepo.listErr = errors.New("query timed out")

	svc := newTestModerationActionServiceWithPenalties(
		actionRepo, newMockActionUserLookup(),
		newMockPenaltyRepoH(), newMockPenaltyGraphH(), newMockPenaltyListerH(),
	)
	h := handler.NewModerationHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/moderation/actions/user-1", nil)
	req = withUser(req, testModerator())
	req = withChiURLParam(req, "user_id", "user-1")
	rec := httptest.NewRecorder()

	h.ListActions(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// --- TakeAction: penalty propagation failure ---

// The action row is written before penalties are propagated, so a vouch-graph
// failure leaves a real, un-retractable action behind. Reporting an error would
// invite the moderator to retry and duplicate it, so the response stays 201 and
// carries the action that was created.
func TestModerationHandler_TakeAction_PenaltyFailureStillReports201(t *testing.T) {
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true}

	graph := newMockPenaltyGraphH()
	graph.err = errors.New("age: connection refused")

	svc := newTestModerationActionService(newMockActionRepoH(), users, newMockPenaltyRepoH(), graph)
	h := handler.NewModerationHandler(svc)

	body := `{"target_user_id":"target-1","action_type":"warn","severity":1,"reason":"first warning"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/moderation/actions", strings.NewReader(body))
	req = withUser(req, testModerator())
	rec := httptest.NewRecorder()

	h.TakeAction(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var result service.TakeActionResult
	decodeBody(t, rec, &result)
	if result.Action == nil || result.Action.TargetUserID != "target-1" {
		t.Fatalf("response = %+v, want the created action", result.Action)
	}
	// The offender's own penalty is written before the graph is consulted, so
	// it survives the failure.
	if len(result.Penalties) != 1 {
		t.Errorf("penalties = %d, want the direct penalty to be reported", len(result.Penalties))
	}
}

// --- LiftMute ---

// mockLiftEnforcer records the muted_until and suspended_until writes the
// handler drives, so a test can tell a nil write (the lift) from no write at
// all — and can tell the two restrictions apart.
type mockLiftEnforcer struct {
	mutes       map[string]*time.Time
	suspensions map[string]*time.Time
	err         error
	suspendErr  error
}

func newMockLiftEnforcer() *mockLiftEnforcer {
	return &mockLiftEnforcer{
		mutes:       make(map[string]*time.Time),
		suspensions: make(map[string]*time.Time),
	}
}

func (m *mockLiftEnforcer) SetUserMutedUntil(_ context.Context, id string, until *time.Time) error {
	if m.err != nil {
		return m.err
	}
	m.mutes[id] = until
	return nil
}

func (m *mockLiftEnforcer) SetUserSuspendedUntil(_ context.Context, id string, until *time.Time) error {
	if m.suspendErr != nil {
		return m.suspendErr
	}
	m.suspensions[id] = until
	return nil
}

func (m *mockLiftEnforcer) UpdateUserRole(_ context.Context, _ string, _ domain.Role) error {
	return nil
}

func (m *mockLiftEnforcer) UpdateUserTrustScore(_ context.Context, _ string, _ float64) error {
	return nil
}

func newTestModerationActionServiceWithEnforcer(
	users service.ActionUserLookup,
	enforcer service.UserEnforcer,
) *service.ModerationActionService {
	modSvc := service.NewModerationService(newMockPenaltyRepoH(), newMockPenaltyGraphH(), func() time.Time { return fixedNow })
	return service.NewModerationActionService(
		newMockActionRepoH(), users, modSvc, enforcer, nil, newMockReliefRepoH(), func() time.Time { return fixedNow })
}

func liftMuteRequest(userID string, caller *domain.User) *http.Request {
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/moderation/users/"+userID+"/mute", nil)
	if caller != nil {
		req = withUser(req, caller)
	}
	return withChiURLParam(req, "user_id", userID)
}

func TestModerationHandler_LiftMute_ClearsTheMuteAndAnswers204(t *testing.T) {
	until := fixedNow.Add(24 * time.Hour)
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, MutedUntil: &until}
	enforcer := newMockLiftEnforcer()
	h := handler.NewModerationHandler(newTestModerationActionServiceWithEnforcer(users, enforcer))

	rec := httptest.NewRecorder()
	h.LiftMute(rec, liftMuteRequest("target-1", testModerator()))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body.String())
	}
	if got, ok := enforcer.mutes["target-1"]; !ok || got != nil {
		t.Errorf("muted_until = %v (written %v), want a nil write", got, ok)
	}
}

// The same answer for a user who was never muted, matching the reaction DELETE:
// the caller asked for a state and the state holds. A 404 or 400 here would
// have the moderation queue report a failure for work that is done.
func TestModerationHandler_LiftMute_AnswersTheSameForAnUnmutedUser(t *testing.T) {
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true}
	h := handler.NewModerationHandler(newTestModerationActionServiceWithEnforcer(users, newMockLiftEnforcer()))

	rec := httptest.NewRecorder()
	h.LiftMute(rec, liftMuteRequest("target-1", testModerator()))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusNoContent, rec.Body.String())
	}
}

func TestModerationHandler_LiftMute_RejectsUnauthenticated(t *testing.T) {
	until := fixedNow.Add(24 * time.Hour)
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, MutedUntil: &until}
	enforcer := newMockLiftEnforcer()
	h := handler.NewModerationHandler(newTestModerationActionServiceWithEnforcer(users, enforcer))

	rec := httptest.NewRecorder()
	h.LiftMute(rec, liftMuteRequest("target-1", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if len(enforcer.mutes) != 0 {
		t.Error("an unauthenticated caller lifted a mute")
	}
}

// The route group guards this, but the handler must not depend on that alone —
// the same reason RemoveByModerator re-checks. A member reaching it must not be
// able to release anyone, themselves least of all.
func TestModerationHandler_LiftMute_RejectsCallersWhoCannotModerate(t *testing.T) {
	tests := []struct {
		name       string
		caller     *domain.User
		targetID   string
		wantStatus int
	}{
		{"a member", &domain.User{ID: "u-1", Role: domain.RoleMember, IsActive: true}, "target-1", http.StatusForbidden},
		{"a member on themselves", &domain.User{ID: "target-1", Role: domain.RoleMember, IsActive: true}, "target-1", http.StatusBadRequest},
		{"a moderator on themselves", &domain.User{ID: "target-1", Role: domain.RoleModerator, IsActive: true}, "target-1", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			until := fixedNow.Add(24 * time.Hour)
			users := newMockActionUserLookup()
			users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, MutedUntil: &until}
			enforcer := newMockLiftEnforcer()
			h := handler.NewModerationHandler(newTestModerationActionServiceWithEnforcer(users, enforcer))

			rec := httptest.NewRecorder()
			h.LiftMute(rec, liftMuteRequest(tt.targetID, tt.caller))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if len(enforcer.mutes) != 0 {
				t.Error("the mute was lifted anyway")
			}
		})
	}
}

func TestModerationHandler_LiftMute_UnknownUserIs404(t *testing.T) {
	h := handler.NewModerationHandler(
		newTestModerationActionServiceWithEnforcer(newMockActionUserLookup(), newMockLiftEnforcer()))

	rec := httptest.NewRecorder()
	h.LiftMute(rec, liftMuteRequest("nobody", testModerator()))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (body %s)", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// A failed write is a mute still in force, so it must not answer 204.
func TestModerationHandler_LiftMute_WriteFailureIs500(t *testing.T) {
	until := fixedNow.Add(24 * time.Hour)
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, MutedUntil: &until}
	enforcer := newMockLiftEnforcer()
	enforcer.err = errors.New("db unavailable")
	h := handler.NewModerationHandler(newTestModerationActionServiceWithEnforcer(users, enforcer))

	rec := httptest.NewRecorder()
	h.LiftMute(rec, liftMuteRequest("target-1", testModerator()))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// --- MuteStatus ---

func muteStatusRequest(userID string, caller *domain.User) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/moderation/users/"+userID+"/mute", nil)
	if caller != nil {
		req = withUser(req, caller)
	}
	return withChiURLParam(req, "user_id", userID)
}

func TestModerationHandler_MuteStatus_ReportsALiveMute(t *testing.T) {
	until := fixedNow.Add(24 * time.Hour)
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, MutedUntil: &until}
	h := handler.NewModerationHandler(newTestModerationActionServiceWithEnforcer(users, newMockLiftEnforcer()))

	rec := httptest.NewRecorder()
	h.MuteStatus(rec, muteStatusRequest("target-1", testModerator()))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp struct {
		MutedUntil string `json:"muted_until"`
	}
	decodeBody(t, rec, &resp)
	if resp.MutedUntil != until.Format(time.RFC3339) {
		t.Errorf("muted_until = %q, want %q", resp.MutedUntil, until.Format(time.RFC3339))
	}
}

// The field's presence is the answer, exactly as on the caller's own profile:
// an unmuted user yields an object with no muted_until rather than a null, so
// one rule reads both shapes.
func TestModerationHandler_MuteStatus_OmitsTheFieldWhenNotMuted(t *testing.T) {
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true}
	h := handler.NewModerationHandler(newTestModerationActionServiceWithEnforcer(users, newMockLiftEnforcer()))

	rec := httptest.NewRecorder()
	h.MuteStatus(rec, muteStatusRequest("target-1", testModerator()))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if strings.Contains(rec.Body.String(), "muted_until") {
		t.Errorf("body = %s, want no muted_until key", rec.Body.String())
	}
}

func TestModerationHandler_MuteStatus_RejectsUnauthenticated(t *testing.T) {
	h := handler.NewModerationHandler(
		newTestModerationActionServiceWithEnforcer(newMockActionUserLookup(), newMockLiftEnforcer()))

	rec := httptest.NewRecorder()
	h.MuteStatus(rec, muteStatusRequest("target-1", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// muted_until is the one field kept off every other user-facing response, so
// the read that exposes it has to refuse anyone without the role that acts on
// it — not merely rely on the route group.
func TestModerationHandler_MuteStatus_RefusesAMember(t *testing.T) {
	until := fixedNow.Add(24 * time.Hour)
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true, MutedUntil: &until}
	h := handler.NewModerationHandler(newTestModerationActionServiceWithEnforcer(users, newMockLiftEnforcer()))

	rec := httptest.NewRecorder()
	h.MuteStatus(rec, muteStatusRequest("target-1", &domain.User{ID: "u-1", Role: domain.RoleMember, IsActive: true}))

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if strings.Contains(rec.Body.String(), until.Format(time.RFC3339)[:10]) {
		t.Errorf("the refusal leaked the mute expiry: %s", rec.Body.String())
	}
}

// --- LiftSuspension ---

func liftSuspensionRequest(userID string, caller *domain.User) *http.Request {
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/moderation/users/"+userID+"/suspension", nil)
	if caller != nil {
		req = withUser(req, caller)
	}
	return withChiURLParam(req, "user_id", userID)
}

// suspendedFor builds the target of these tests. is_active is false because that
// is what a suspended user looks like coming out of the repository; nothing in
// the lift reads it.
func suspendedFor(id string) *domain.User {
	until := fixedNow.Add(24 * time.Hour)
	return &domain.User{ID: id, IsActive: false, SuspendedUntil: &until}
}

func TestModerationHandler_LiftSuspension_ClearsTheSuspensionAndAnswers204(t *testing.T) {
	users := newMockActionUserLookup()
	users.users["target-1"] = suspendedFor("target-1")
	enforcer := newMockLiftEnforcer()
	h := handler.NewModerationHandler(newTestModerationActionServiceWithEnforcer(users, enforcer))

	rec := httptest.NewRecorder()
	h.LiftSuspension(rec, liftSuspensionRequest("target-1", testModerator()))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body.String())
	}
	if got, ok := enforcer.suspensions["target-1"]; !ok || got != nil {
		t.Errorf("suspended_until = %v (written %v), want a nil write", got, ok)
	}
	// The two restrictions are separate columns and a lift of one must not
	// disturb the other: a suspended member may also be muted.
	if len(enforcer.mutes) != 0 {
		t.Errorf("lifting a suspension wrote muted_until: %v", enforcer.mutes)
	}
}

// The same answer for a user who was not suspended, matching the mute DELETE:
// the caller asked for a state and the state holds.
func TestModerationHandler_LiftSuspension_AnswersTheSameForAnUnsuspendedUser(t *testing.T) {
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true}
	h := handler.NewModerationHandler(newTestModerationActionServiceWithEnforcer(users, newMockLiftEnforcer()))

	rec := httptest.NewRecorder()
	h.LiftSuspension(rec, liftSuspensionRequest("target-1", testModerator()))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusNoContent, rec.Body.String())
	}
}

func TestModerationHandler_LiftSuspension_RejectsUnauthenticated(t *testing.T) {
	users := newMockActionUserLookup()
	users.users["target-1"] = suspendedFor("target-1")
	enforcer := newMockLiftEnforcer()
	h := handler.NewModerationHandler(newTestModerationActionServiceWithEnforcer(users, enforcer))

	rec := httptest.NewRecorder()
	h.LiftSuspension(rec, liftSuspensionRequest("target-1", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if len(enforcer.suspensions) != 0 {
		t.Error("an unauthenticated caller lifted a suspension")
	}
}

// The route group guards this, but the handler must not depend on that alone.
// Self-release is the case worth pinning: a moderator must not be able to
// overturn a colleague's decision about them.
func TestModerationHandler_LiftSuspension_RejectsCallersWhoCannotModerate(t *testing.T) {
	tests := []struct {
		name       string
		caller     *domain.User
		targetID   string
		wantStatus int
	}{
		{"a member", &domain.User{ID: "u-1", Role: domain.RoleMember, IsActive: true}, "target-1", http.StatusForbidden},
		{"a member on themselves", &domain.User{ID: "target-1", Role: domain.RoleMember, IsActive: true}, "target-1", http.StatusBadRequest},
		{"a moderator on themselves", &domain.User{ID: "target-1", Role: domain.RoleModerator, IsActive: true}, "target-1", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users := newMockActionUserLookup()
			users.users["target-1"] = suspendedFor("target-1")
			enforcer := newMockLiftEnforcer()
			h := handler.NewModerationHandler(newTestModerationActionServiceWithEnforcer(users, enforcer))

			rec := httptest.NewRecorder()
			h.LiftSuspension(rec, liftSuspensionRequest(tt.targetID, tt.caller))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if len(enforcer.suspensions) != 0 {
				t.Error("the suspension was lifted anyway")
			}
		})
	}
}

func TestModerationHandler_LiftSuspension_UnknownUserIs404(t *testing.T) {
	h := handler.NewModerationHandler(
		newTestModerationActionServiceWithEnforcer(newMockActionUserLookup(), newMockLiftEnforcer()))

	rec := httptest.NewRecorder()
	h.LiftSuspension(rec, liftSuspensionRequest("nobody", testModerator()))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (body %s)", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// A failed write is a suspension still in force, so it must not answer 204.
func TestModerationHandler_LiftSuspension_WriteFailureIs500(t *testing.T) {
	users := newMockActionUserLookup()
	users.users["target-1"] = suspendedFor("target-1")
	enforcer := newMockLiftEnforcer()
	enforcer.suspendErr = errors.New("db unavailable")
	h := handler.NewModerationHandler(newTestModerationActionServiceWithEnforcer(users, enforcer))

	rec := httptest.NewRecorder()
	h.LiftSuspension(rec, liftSuspensionRequest("target-1", testModerator()))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// --- SuspensionStatus ---

func suspensionStatusRequest(userID string, caller *domain.User) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/moderation/users/"+userID+"/suspension", nil)
	if caller != nil {
		req = withUser(req, caller)
	}
	return withChiURLParam(req, "user_id", userID)
}

func TestModerationHandler_SuspensionStatus_ReportsALiveSuspension(t *testing.T) {
	users := newMockActionUserLookup()
	users.users["target-1"] = suspendedFor("target-1")
	h := handler.NewModerationHandler(newTestModerationActionServiceWithEnforcer(users, newMockLiftEnforcer()))

	rec := httptest.NewRecorder()
	h.SuspensionStatus(rec, suspensionStatusRequest("target-1", testModerator()))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp struct {
		SuspendedUntil string `json:"suspended_until"`
	}
	decodeBody(t, rec, &resp)
	want := fixedNow.Add(24 * time.Hour).Format(time.RFC3339)
	if resp.SuspendedUntil != want {
		t.Errorf("suspended_until = %q, want %q", resp.SuspendedUntil, want)
	}
}

// The field's presence is the answer, exactly as for the mute: an unsuspended
// user yields an object with no suspended_until rather than a null.
func TestModerationHandler_SuspensionStatus_OmitsTheFieldWhenNotSuspended(t *testing.T) {
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true}
	h := handler.NewModerationHandler(newTestModerationActionServiceWithEnforcer(users, newMockLiftEnforcer()))

	rec := httptest.NewRecorder()
	h.SuspensionStatus(rec, suspensionStatusRequest("target-1", testModerator()))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if strings.Contains(rec.Body.String(), "suspended_until") {
		t.Errorf("body = %s, want no suspended_until key", rec.Body.String())
	}
}

func TestModerationHandler_SuspensionStatus_RejectsUnauthenticated(t *testing.T) {
	h := handler.NewModerationHandler(
		newTestModerationActionServiceWithEnforcer(newMockActionUserLookup(), newMockLiftEnforcer()))

	rec := httptest.NewRecorder()
	h.SuspensionStatus(rec, suspensionStatusRequest("target-1", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// suspended_until is moderation state kept off every user-facing response, so
// the read that exposes it refuses anyone without the role that acts on it.
func TestModerationHandler_SuspensionStatus_RefusesAMember(t *testing.T) {
	users := newMockActionUserLookup()
	users.users["target-1"] = suspendedFor("target-1")
	h := handler.NewModerationHandler(newTestModerationActionServiceWithEnforcer(users, newMockLiftEnforcer()))

	rec := httptest.NewRecorder()
	h.SuspensionStatus(rec, suspensionStatusRequest("target-1", &domain.User{ID: "u-1", Role: domain.RoleMember, IsActive: true}))

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if strings.Contains(rec.Body.String(), fixedNow.Add(24 * time.Hour).Format(time.RFC3339)[:10]) {
		t.Errorf("the refusal leaked the suspension expiry: %s", rec.Body.String())
	}
}

// --- display names on the audit trail ---

// The trail names two people, and a moderator reading it should not have to look
// either id up by hand. The names ride along from the query's joins; the ids
// stay, because the moderator views link to profiles by id.
func TestModerationHandler_ListActions_CarriesDisplayNames(t *testing.T) {
	actionRepo := newMockActionRepoH()
	actionRepo.actionsByTarget = []*domain.ModerationAction{{
		ID: "act-1", TargetUserID: "user-1", ModeratorID: "mod-1",
		Action: domain.ActionWarn, Severity: 1, Reason: "spam", CreatedAt: fixedNow,
		TargetDisplayName: "Alice", ModeratorDisplayName: "Mallory",
	}}
	h := handler.NewModerationHandler(newTestModerationActionService(
		actionRepo, newMockActionUserLookup(), newMockPenaltyRepoH(), newMockPenaltyGraphH()))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/moderation/actions/user-1", nil)
	req = withUser(req, testModerator())
	req = withChiURLParam(req, "user_id", "user-1")
	rec := httptest.NewRecorder()

	h.ListActions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Actions []struct {
			Action struct {
				TargetUserID         string `json:"target_user_id"`
				TargetDisplayName    string `json:"target_display_name"`
				ModeratorID          string `json:"moderator_id"`
				ModeratorDisplayName string `json:"moderator_display_name"`
			} `json:"action"`
		} `json:"actions"`
	}
	decodeBody(t, rec, &resp)
	if len(resp.Actions) != 1 {
		t.Fatalf("got %d actions, want 1", len(resp.Actions))
	}
	got := resp.Actions[0].Action
	if got.TargetDisplayName != "Alice" {
		t.Errorf("target_display_name = %q, want Alice", got.TargetDisplayName)
	}
	if got.ModeratorDisplayName != "Mallory" {
		t.Errorf("moderator_display_name = %q, want Mallory", got.ModeratorDisplayName)
	}
	if got.TargetUserID != "user-1" || got.ModeratorID != "mod-1" {
		t.Errorf("ids = %q/%q, want user-1/mod-1", got.TargetUserID, got.ModeratorID)
	}
}

// The action a create echoes back carries no names: the create path knows both
// people by id only, and two empty strings there would read as two nameless
// members rather than as "not looked up".
func TestModerationHandler_TakeAction_OmitsDisplayNames(t *testing.T) {
	users := newMockActionUserLookup()
	users.users["target-1"] = &domain.User{ID: "target-1", IsActive: true}
	h := handler.NewModerationHandler(newTestModerationActionService(
		newMockActionRepoH(), users, newMockPenaltyRepoH(), newMockPenaltyGraphH()))

	body := `{"target_user_id":"target-1","action_type":"warn","severity":1,"reason":"first warning"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/moderation/actions", strings.NewReader(body))
	req = withUser(req, testModerator())
	rec := httptest.NewRecorder()

	h.TakeAction(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "display_name") {
		t.Errorf("body = %s, want no display name keys", rec.Body.String())
	}
}

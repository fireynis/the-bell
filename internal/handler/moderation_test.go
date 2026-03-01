package handler_test

import (
	"context"
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
}

func newMockActionRepoH() *mockActionRepo {
	return &mockActionRepo{}
}

func (m *mockActionRepo) CreateModerationAction(_ context.Context, action *domain.ModerationAction) error {
	m.actions = append(m.actions, action)
	return nil
}

func (m *mockActionRepo) ListActionsByTarget(_ context.Context, _ string, _, _ int) ([]*domain.ModerationAction, error) {
	return m.actionsByTarget, nil
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

func (m *mockPenaltyListerH) ListPenaltiesByActionID(_ context.Context, actionID string) ([]domain.TrustPenalty, error) {
	return m.penalties[actionID], nil
}

// --- mock PenaltyGraphQuerier ---

type mockPenaltyGraph struct {
	vouchers map[string]int
}

func newMockPenaltyGraphH() *mockPenaltyGraph {
	return &mockPenaltyGraph{vouchers: make(map[string]int)}
}

func (m *mockPenaltyGraph) FindVouchersWithDepth(_ context.Context, _ string, _ int) (map[string]int, error) {
	return m.vouchers, nil
}

// --- test helpers ---

func newTestModerationActionService(
	actions service.ModerationActionRepository,
	users service.ActionUserLookup,
	penalties service.PenaltyRepository,
	graph service.PenaltyGraphQuerier,
) *service.ModerationActionService {
	modSvc := service.NewModerationService(penalties, graph, func() time.Time { return fixedNow })
	return service.NewModerationActionService(actions, users, modSvc, nil, nil, func() time.Time { return fixedNow })
}

func newTestModerationActionServiceWithPenalties(
	actions service.ModerationActionRepository,
	users service.ActionUserLookup,
	penalties service.PenaltyRepository,
	graph service.PenaltyGraphQuerier,
	penaltyLister service.PenaltyLister,
) *service.ModerationActionService {
	modSvc := service.NewModerationService(penalties, graph, func() time.Time { return fixedNow })
	return service.NewModerationActionService(actions, users, modSvc, nil, penaltyLister, func() time.Time { return fixedNow })
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

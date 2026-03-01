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
	actions []*domain.ModerationAction
}

func newMockActionRepoH() *mockActionRepo {
	return &mockActionRepo{}
}

func (m *mockActionRepo) CreateModerationAction(_ context.Context, action *domain.ModerationAction) error {
	m.actions = append(m.actions, action)
	return nil
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
	return service.NewModerationActionService(actions, users, modSvc, nil, func() time.Time { return fixedNow })
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

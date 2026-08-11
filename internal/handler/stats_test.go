package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fireynis/the-bell/internal/handler"
	"github.com/fireynis/the-bell/internal/service"
)

type stubStatsGetter struct {
	stats *service.TownStats
	err   error
}

func (s *stubStatsGetter) GetStats(_ context.Context) (*service.TownStats, error) {
	return s.stats, s.err
}

func TestStatsHandler_GetStats(t *testing.T) {
	stub := &stubStatsGetter{
		stats: &service.TownStats{
			TotalUsers:       42,
			PostsToday:       7,
			ActiveModerators: 3,
			PendingUsers:     2,
		},
	}

	h := handler.NewStatsHandler(stub)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/stats", nil)
	w := httptest.NewRecorder()

	h.GetStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if body == "" {
		t.Fatal("expected non-empty body")
	}
	// The admin dashboard reads these keys by name, so the JSON tags are part
	// of the contract and not an implementation detail of TownStats.
	for _, field := range []string{"total_users", "posts_today", "active_moderators", "pending_users"} {
		if !strings.Contains(body, field) {
			t.Errorf("expected body to contain %q, got %s", field, body)
		}
	}
}

// The stats query touches several tables, so a database failure here is
// ordinary. It has to arrive as a 500 with the fixed message: the underlying
// error is a SQL string naming tables and columns, and this endpoint is
// reachable by anyone with the council role.
func TestStatsHandler_GetStats_ServiceError(t *testing.T) {
	stub := &stubStatsGetter{err: errors.New("counting users: relation \"users\" does not exist")}

	h := handler.NewStatsHandler(stub)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/stats", nil)
	w := httptest.NewRecorder()

	h.GetStats(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}

	body := w.Body.String()
	if strings.Contains(body, "relation") || strings.Contains(body, "users\" does not") {
		t.Errorf("response leaked the underlying error: %s", body)
	}

	var resp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("error response is not JSON: %v (%s)", err, body)
	}
	if resp.Error != "internal error" {
		t.Errorf("error = %q, want %q", resp.Error, "internal error")
	}
}

// A ErrNotFound-shaped failure must not become a 404 on a listing endpoint
// that always has an answer — the mapping is shared with every other handler,
// so this pins that stats go through it rather than around it.
func TestStatsHandler_GetStats_MapsSentinelErrors(t *testing.T) {
	stub := &stubStatsGetter{err: service.ErrForbidden}

	h := handler.NewStatsHandler(stub)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/stats", nil)
	w := httptest.NewRecorder()

	h.GetStats(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

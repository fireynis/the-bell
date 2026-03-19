package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	// Check that all expected fields are present
	for _, field := range []string{"total_users", "posts_today", "active_moderators", "pending_users"} {
		if !contains(body, field) {
			t.Errorf("expected body to contain %q, got %s", field, body)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

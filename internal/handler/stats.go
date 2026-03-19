package handler

import (
	"context"
	"net/http"

	"github.com/fireynis/the-bell/internal/service"
)

// StatsGetter defines the operations needed by the stats handler.
type StatsGetter interface {
	GetStats(ctx context.Context) (*service.TownStats, error)
}

// StatsHandler handles HTTP requests for admin statistics.
type StatsHandler struct {
	stats StatsGetter
}

// NewStatsHandler creates a StatsHandler.
func NewStatsHandler(stats StatsGetter) *StatsHandler {
	return &StatsHandler{stats: stats}
}

// GetStats handles GET /api/v1/admin/stats.
func (h *StatsHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.stats.GetStats(r.Context())
	if err != nil {
		serviceError(w, err)
		return
	}

	JSON(w, http.StatusOK, stats)
}

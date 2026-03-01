package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/service"
)

// ReportHandler handles HTTP requests for report operations.
type ReportHandler struct {
	reports *service.ReportService
}

// NewReportHandler creates a ReportHandler.
func NewReportHandler(reports *service.ReportService) *ReportHandler {
	return &ReportHandler{reports: reports}
}

type submitReportRequest struct {
	Reason string `json:"reason"`
}

type listQueueResponse struct {
	Reports []*domain.Report `json:"reports"`
}

// SubmitReport handles POST /api/v1/posts/{id}/report.
func (h *ReportHandler) SubmitReport(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	postID := chi.URLParam(r, "id")

	var req submitReportRequest
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	report, err := h.reports.SubmitReport(r.Context(), user.ID, postID, req.Reason)
	if err != nil {
		serviceError(w, err)
		return
	}

	JSON(w, http.StatusCreated, report)
}

// ListQueue handles GET /api/v1/moderation/queue.
func (h *ReportHandler) ListQueue(w http.ResponseWriter, r *http.Request) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	offset := parseOffset(r.URL.Query().Get("offset"))

	reports, err := h.reports.ListQueue(r.Context(), limit, offset)
	if err != nil {
		serviceError(w, err)
		return
	}

	if reports == nil {
		reports = []*domain.Report{}
	}

	JSON(w, http.StatusOK, listQueueResponse{Reports: reports})
}

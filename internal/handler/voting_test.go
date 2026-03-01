package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/service"
)

type mockVotingService struct {
	castResult *domain.ProposalSummary
	castErr    error
	listResult []domain.ProposalSummary
	listErr    error
}

func (m *mockVotingService) CastVote(_ context.Context, proposalID, voterID string, choice domain.VoteChoice) (*domain.ProposalSummary, error) {
	if m.castErr != nil {
		return nil, m.castErr
	}
	return m.castResult, nil
}

func (m *mockVotingService) ListPendingProposals(_ context.Context) ([]domain.ProposalSummary, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.listResult, nil
}

func TestVotingHandler_CastVote_Success(t *testing.T) {
	svc := &mockVotingService{
		castResult: &domain.ProposalSummary{
			ProposalID:   "promote:user-1",
			ApproveCount: 1,
			RejectCount:  0,
			TotalCouncil: 3,
			Status:       domain.ProposalPending,
		},
	}
	h := NewVotingHandler(svc)

	body := `{"proposal_id":"promote:user-1","vote":"approve"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/council/votes", strings.NewReader(body))
	ctx := middleware.WithUser(req.Context(), &domain.User{
		ID: "council-1", Role: domain.RoleCouncil, IsActive: true,
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.CastVote(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusCreated)
	}

	var resp domain.ProposalSummary
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.ProposalID != "promote:user-1" {
		t.Errorf("ProposalID = %q, want %q", resp.ProposalID, "promote:user-1")
	}
}

func TestVotingHandler_CastVote_Unauthorized(t *testing.T) {
	svc := &mockVotingService{}
	h := NewVotingHandler(svc)

	body := `{"proposal_id":"promote:user-1","vote":"approve"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/council/votes", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.CastVote(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestVotingHandler_CastVote_BadRequest(t *testing.T) {
	svc := &mockVotingService{}
	h := NewVotingHandler(svc)

	body := `{"proposal_id":"","vote":"approve"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/council/votes", strings.NewReader(body))
	ctx := middleware.WithUser(req.Context(), &domain.User{
		ID: "council-1", Role: domain.RoleCouncil, IsActive: true,
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.CastVote(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestVotingHandler_CastVote_ValidationError(t *testing.T) {
	svc := &mockVotingService{
		castErr: service.ErrValidation,
	}
	h := NewVotingHandler(svc)

	body := `{"proposal_id":"promote:user-1","vote":"approve"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/council/votes", strings.NewReader(body))
	ctx := middleware.WithUser(req.Context(), &domain.User{
		ID: "council-1", Role: domain.RoleCouncil, IsActive: true,
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.CastVote(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestVotingHandler_ListPending_Success(t *testing.T) {
	svc := &mockVotingService{
		listResult: []domain.ProposalSummary{
			{
				ProposalID:   "promote:user-1",
				ApproveCount: 1,
				RejectCount:  0,
				TotalCouncil: 3,
				Status:       domain.ProposalPending,
			},
		},
	}
	h := NewVotingHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/council/votes", nil)
	ctx := middleware.WithUser(req.Context(), &domain.User{
		ID: "council-1", Role: domain.RoleCouncil, IsActive: true,
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ListPending(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Proposals []domain.ProposalSummary `json:"proposals"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(resp.Proposals) != 1 {
		t.Fatalf("len(proposals) = %d, want 1", len(resp.Proposals))
	}
}

func TestVotingHandler_ListPending_Empty(t *testing.T) {
	svc := &mockVotingService{listResult: nil}
	h := NewVotingHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/council/votes", nil)
	ctx := middleware.WithUser(req.Context(), &domain.User{
		ID: "council-1", Role: domain.RoleCouncil, IsActive: true,
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ListPending(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Proposals []domain.ProposalSummary `json:"proposals"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Proposals == nil || len(resp.Proposals) != 0 {
		t.Errorf("expected empty array, got %v", resp.Proposals)
	}
}

func TestVotingHandler_ListPending_ServiceError(t *testing.T) {
	svc := &mockVotingService{
		listErr: service.ErrForbidden,
	}
	h := NewVotingHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/council/votes", nil)
	ctx := middleware.WithUser(req.Context(), &domain.User{
		ID: "council-1", Role: domain.RoleCouncil, IsActive: true,
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ListPending(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

package handler

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
)

// ProposalService defines the operations needed by the proposal handler.
type ProposalService interface {
	Create(ctx context.Context, creator *domain.User, t domain.ProposalType, targetID, rationale string) (*domain.ProposalView, error)
	Vote(ctx context.Context, voter *domain.User, proposalID string, approve bool) (*domain.ProposalView, error)
	List(ctx context.Context, viewer *domain.User, decided bool, limit int) ([]domain.ProposalView, error)
}

// ProposalHandler serves the council's Town Hall: raising motions, voting on
// them, and reading the queue.
type ProposalHandler struct {
	proposals ProposalService
}

func NewProposalHandler(proposals ProposalService) *ProposalHandler {
	return &ProposalHandler{proposals: proposals}
}

// decidedProposalLimit bounds the decided listing. The council's recent record
// is what the screen shows; the full history of every motion the town has ever
// settled is not something one request should return.
const decidedProposalLimit = 50

// proposalResponse is the published shape of one motion. It is spelled out
// rather than serializing domain.ProposalView so that the wire format is a
// decision made here, not a side effect of a domain field being renamed.
type proposalResponse struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	// TargetUserID and TargetDisplayName are omitted entirely for a motion
	// about the town rather than a person, so their absence is the answer to
	// "is this about somebody".
	TargetUserID         string `json:"target_user_id,omitempty"`
	TargetDisplayName    string `json:"target_display_name,omitempty"`
	Rationale            string `json:"rationale"`
	CreatedBy            string `json:"created_by"`
	CreatedByDisplayName string `json:"created_by_display_name"`
	Status               string `json:"status"`
	CreatedAt            string `json:"created_at"`
	DecidedAt            string `json:"decided_at,omitempty"`
	ApproveCount         int64  `json:"approve_count"`
	RejectCount          int64  `json:"reject_count"`
	// CouncilSize is the electorate for THIS motion — the council minus the
	// person being removed, on a removal — not the size of the council. A
	// client rendering progress towards a majority must use this number or it
	// will draw a threshold nobody can reach.
	CouncilSize int64 `json:"council_size"`
	// MyVote is the caller's own choice, or null. It is a pointer without
	// omitempty because "has not voted" is an answer the client acts on, and a
	// missing key would make it indistinguishable from a field the server
	// forgot.
	MyVote *string `json:"my_vote"`
}

type listProposalsResponse struct {
	Proposals []proposalResponse `json:"proposals"`
}

type createProposalRequest struct {
	Type         string `json:"type"`
	TargetUserID string `json:"target_user_id"`
	Rationale    string `json:"rationale"`
}

type castVoteRequest struct {
	Approve bool `json:"approve"`
}

func toProposalResponse(v domain.ProposalView) proposalResponse {
	resp := proposalResponse{
		ID:                   v.ID,
		Type:                 string(v.Type),
		TargetUserID:         v.TargetUserID,
		TargetDisplayName:    v.TargetDisplayName,
		Rationale:            v.Rationale,
		CreatedBy:            v.CreatedBy,
		CreatedByDisplayName: v.CreatedByDisplayName,
		Status:               string(v.Status),
		CreatedAt:            v.CreatedAt.Format(timestampFormat),
		ApproveCount:         v.ApproveCount,
		RejectCount:          v.RejectCount,
		CouncilSize:          v.CouncilSize,
	}
	if v.DecidedAt != nil {
		resp.DecidedAt = v.DecidedAt.Format(timestampFormat)
	}
	if v.MyVote != nil {
		choice := string(*v.MyVote)
		resp.MyVote = &choice
	}
	return resp
}

// List handles GET /api/v1/admin/proposals?status=open|decided.
//
// Anything other than status=decided lists the open motions, including an
// absent or misspelled status. The open queue is what the council came for and
// what an unqualified request should get; failing a typo with a 400 would hide
// live business behind a spelling mistake.
func (h *ProposalHandler) List(w http.ResponseWriter, r *http.Request) {
	viewer, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	decided := r.URL.Query().Get("status") == "decided"

	views, err := h.proposals.List(r.Context(), viewer, decided, decidedProposalLimit)
	if err != nil {
		serviceError(w, err)
		return
	}

	proposals := make([]proposalResponse, 0, len(views))
	for _, v := range views {
		proposals = append(proposals, toProposalResponse(v))
	}

	JSON(w, http.StatusOK, listProposalsResponse{Proposals: proposals})
}

// Create handles POST /api/v1/admin/proposals.
func (h *ProposalHandler) Create(w http.ResponseWriter, r *http.Request) {
	creator, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createProposalRequest
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	view, err := h.proposals.Create(r.Context(), creator,
		domain.ProposalType(req.Type), req.TargetUserID, req.Rationale)
	if err != nil {
		serviceError(w, err)
		return
	}

	JSON(w, http.StatusCreated, toProposalResponse(*view))
}

// Vote handles POST /api/v1/admin/proposals/{id}/votes.
//
// It answers with the whole motion rather than an acknowledgement, so a council
// member who casts the deciding vote sees the new tally and the outcome —
// including a promotion that has just taken effect — in the same response.
func (h *ProposalHandler) Vote(w http.ResponseWriter, r *http.Request) {
	voter, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req castVoteRequest
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	view, err := h.proposals.Vote(r.Context(), voter, chi.URLParam(r, "id"), req.Approve)
	if err != nil {
		serviceError(w, err)
		return
	}

	JSON(w, http.StatusOK, toProposalResponse(*view))
}

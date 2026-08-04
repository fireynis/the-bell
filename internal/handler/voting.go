package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
)

// VotingService defines the operations needed by the voting handler.
type VotingService interface {
	CastVote(ctx context.Context, proposalID, voterID string, choice domain.VoteChoice) (*domain.ProposalSummary, error)
	ListPendingProposals(ctx context.Context) ([]domain.ProposalSummary, error)
}

// VotingHandler handles HTTP requests for council voting.
type VotingHandler struct {
	voting VotingService
}

func NewVotingHandler(voting VotingService) *VotingHandler {
	return &VotingHandler{voting: voting}
}

type castVoteRequest struct {
	ProposalID string `json:"proposal_id"`
	Vote       string `json:"vote"`
}

type listProposalsResponse struct {
	Proposals []domain.ProposalSummary `json:"proposals"`
}

var (
	errMissingProposalID = errors.New("proposal_id is required")
	errInvalidVoteChoice = errors.New("vote must be 'approve' or 'reject'")
)

// validateCastVote checks a cast-vote request before it reaches the service.
//
// The choice is matched against the two known values here so that an unknown
// one is rejected as a bad request rather than recorded as a vote that no
// tally will ever count.
func validateCastVote(req castVoteRequest) error {
	if req.ProposalID == "" {
		return errMissingProposalID
	}
	if req.Vote != string(domain.VoteApprove) && req.Vote != string(domain.VoteReject) {
		return errInvalidVoteChoice
	}
	return nil
}

// CastVote handles POST /api/v1/admin/council/votes.
func (h *VotingHandler) CastVote(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req castVoteRequest
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := validateCastVote(req); err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}

	summary, err := h.voting.CastVote(r.Context(), req.ProposalID, user.ID, domain.VoteChoice(req.Vote))
	if err != nil {
		serviceError(w, err)
		return
	}

	JSON(w, http.StatusCreated, summary)
}

// ListPending handles GET /api/v1/admin/council/votes.
func (h *VotingHandler) ListPending(w http.ResponseWriter, r *http.Request) {
	proposals, err := h.voting.ListPendingProposals(r.Context())
	if err != nil {
		serviceError(w, err)
		return
	}

	if proposals == nil {
		proposals = []domain.ProposalSummary{}
	}

	JSON(w, http.StatusOK, listProposalsResponse{Proposals: proposals})
}

package service

import (
	"context"
	"fmt"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/google/uuid"
)

// VoteRepository is the subset of vote persistence needed by VotingService.
type VoteRepository interface {
	CreateVote(ctx context.Context, vote *domain.CouncilVote) error
	GetVoteByProposalAndVoter(ctx context.Context, proposalID, voterID string) (*domain.CouncilVote, error)
	ListVotesByProposal(ctx context.Context, proposalID string) ([]domain.CouncilVote, error)
	CountVotes(ctx context.Context, proposalID string, vote domain.VoteChoice) (int64, error)
	ListOpenProposalIDs(ctx context.Context) ([]string, error)
	CountCouncilMembers(ctx context.Context) (int64, error)
}

// VotingService handles council voting on proposals.
type VotingService struct {
	votes VoteRepository
	now   func() time.Time
}

func NewVotingService(votes VoteRepository, clock func() time.Time) *VotingService {
	if clock == nil {
		clock = time.Now
	}
	return &VotingService{votes: votes, now: clock}
}

// CastVote records a council member's vote on a proposal and returns the
// current proposal summary including updated status.
func (s *VotingService) CastVote(ctx context.Context, proposalID, voterID string, choice domain.VoteChoice) (*domain.ProposalSummary, error) {
	if proposalID == "" {
		return nil, fmt.Errorf("%w: proposal_id is required", ErrValidation)
	}
	if choice != domain.VoteApprove && choice != domain.VoteReject {
		return nil, fmt.Errorf("%w: vote must be 'approve' or 'reject'", ErrValidation)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generating vote id: %w", err)
	}

	vote := &domain.CouncilVote{
		ID:         id.String(),
		ProposalID: proposalID,
		VoterID:    voterID,
		Vote:       choice,
		CreatedAt:  s.now(),
	}

	if err := s.votes.CreateVote(ctx, vote); err != nil {
		return nil, fmt.Errorf("casting vote: %w", err)
	}

	return s.buildSummary(ctx, proposalID)
}

// ListPendingProposals returns summaries for all proposals that have votes.
func (s *VotingService) ListPendingProposals(ctx context.Context) ([]domain.ProposalSummary, error) {
	ids, err := s.votes.ListOpenProposalIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing proposal ids: %w", err)
	}

	summaries := make([]domain.ProposalSummary, 0, len(ids))
	for _, id := range ids {
		summary, err := s.buildSummary(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("building summary for %s: %w", id, err)
		}
		summaries = append(summaries, *summary)
	}
	return summaries, nil
}

func (s *VotingService) buildSummary(ctx context.Context, proposalID string) (*domain.ProposalSummary, error) {
	approveCount, err := s.votes.CountVotes(ctx, proposalID, domain.VoteApprove)
	if err != nil {
		return nil, fmt.Errorf("counting approvals: %w", err)
	}
	rejectCount, err := s.votes.CountVotes(ctx, proposalID, domain.VoteReject)
	if err != nil {
		return nil, fmt.Errorf("counting rejections: %w", err)
	}
	totalCouncil, err := s.votes.CountCouncilMembers(ctx)
	if err != nil {
		return nil, fmt.Errorf("counting council members: %w", err)
	}

	votes, err := s.votes.ListVotesByProposal(ctx, proposalID)
	if err != nil {
		return nil, fmt.Errorf("listing votes: %w", err)
	}

	majority := totalCouncil/2 + 1
	status := domain.ProposalPending
	if approveCount >= majority {
		status = domain.ProposalApproved
	} else if rejectCount >= majority {
		status = domain.ProposalRejected
	}

	return &domain.ProposalSummary{
		ProposalID:   proposalID,
		ApproveCount: approveCount,
		RejectCount:  rejectCount,
		TotalCouncil: totalCouncil,
		Status:       status,
		Votes:        votes,
	}, nil
}

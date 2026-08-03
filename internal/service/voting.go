package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/google/uuid"
)

// simpleMajority returns the number of votes needed to carry a proposal for a
// council of the given size: strictly more than half.
func simpleMajority(councilSize int64) int64 {
	if councilSize <= 0 {
		return 0
	}
	return councilSize/2 + 1
}

// evaluateProposal decides a proposal's status from its tallies. A proposal is
// decided as soon as either side reaches a simple majority of the whole
// council; approval is checked first so a council that somehow recorded a
// majority both ways resolves to approved.
//
// A council size of zero has no reachable majority and always stays pending,
// which keeps an empty or miscounted council from auto-approving everything.
func evaluateProposal(approveCount, rejectCount, councilSize int64) domain.ProposalStatus {
	majority := simpleMajority(councilSize)
	if majority <= 0 {
		return domain.ProposalPending
	}
	switch {
	case approveCount >= majority:
		return domain.ProposalApproved
	case rejectCount >= majority:
		return domain.ProposalRejected
	default:
		return domain.ProposalPending
	}
}

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

	existing, err := s.votes.GetVoteByProposalAndVoter(ctx, proposalID, voterID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("checking existing vote: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("%w: you have already voted on this proposal", ErrValidation)
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

	council, err := s.votes.CountCouncilMembers(ctx)
	if err != nil {
		return nil, fmt.Errorf("counting council members: %w", err)
	}

	return s.buildSummary(ctx, proposalID, council)
}

// ListPendingProposals returns summaries for all proposals that have votes.
func (s *VotingService) ListPendingProposals(ctx context.Context) ([]domain.ProposalSummary, error) {
	ids, err := s.votes.ListOpenProposalIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing proposal ids: %w", err)
	}

	totalCouncil, err := s.votes.CountCouncilMembers(ctx)
	if err != nil {
		return nil, fmt.Errorf("counting council members: %w", err)
	}

	summaries := make([]domain.ProposalSummary, 0, len(ids))
	for _, id := range ids {
		summary, err := s.buildSummary(ctx, id, totalCouncil)
		if err != nil {
			return nil, fmt.Errorf("building summary for %s: %w", id, err)
		}
		summaries = append(summaries, *summary)
	}
	return summaries, nil
}

// buildSummary gathers the tallies for one proposal. The caller supplies the
// council size so that listing many proposals counts the council only once.
func (s *VotingService) buildSummary(ctx context.Context, proposalID string, councilSize int64) (*domain.ProposalSummary, error) {
	approveCount, err := s.votes.CountVotes(ctx, proposalID, domain.VoteApprove)
	if err != nil {
		return nil, fmt.Errorf("counting approvals: %w", err)
	}
	rejectCount, err := s.votes.CountVotes(ctx, proposalID, domain.VoteReject)
	if err != nil {
		return nil, fmt.Errorf("counting rejections: %w", err)
	}

	votes, err := s.votes.ListVotesByProposal(ctx, proposalID)
	if err != nil {
		return nil, fmt.Errorf("listing votes: %w", err)
	}

	return &domain.ProposalSummary{
		ProposalID:   proposalID,
		ApproveCount: approveCount,
		RejectCount:  rejectCount,
		TotalCouncil: councilSize,
		Status:       evaluateProposal(approveCount, rejectCount, councilSize),
		Votes:        votes,
	}, nil
}

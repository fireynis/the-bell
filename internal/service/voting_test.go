package service

import (
	"context"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

type mockVoteRepo struct {
	votes        map[string][]domain.CouncilVote // proposalID -> votes
	councilCount int64
	createErr    error
}

func newMockVoteRepo() *mockVoteRepo {
	return &mockVoteRepo{
		votes:        make(map[string][]domain.CouncilVote),
		councilCount: 3,
	}
}

func (m *mockVoteRepo) CreateVote(_ context.Context, vote *domain.CouncilVote) error {
	if m.createErr != nil {
		return m.createErr
	}
	for _, v := range m.votes[vote.ProposalID] {
		if v.VoterID == vote.VoterID {
			return ErrValidation
		}
	}
	m.votes[vote.ProposalID] = append(m.votes[vote.ProposalID], *vote)
	return nil
}

func (m *mockVoteRepo) GetVoteByProposalAndVoter(_ context.Context, proposalID, voterID string) (*domain.CouncilVote, error) {
	for _, v := range m.votes[proposalID] {
		if v.VoterID == voterID {
			return &v, nil
		}
	}
	return nil, ErrNotFound
}

func (m *mockVoteRepo) ListVotesByProposal(_ context.Context, proposalID string) ([]domain.CouncilVote, error) {
	return m.votes[proposalID], nil
}

func (m *mockVoteRepo) CountVotes(_ context.Context, proposalID string, vote domain.VoteChoice) (int64, error) {
	var count int64
	for _, v := range m.votes[proposalID] {
		if v.Vote == vote {
			count++
		}
	}
	return count, nil
}

func (m *mockVoteRepo) ListOpenProposalIDs(_ context.Context) ([]string, error) {
	var ids []string
	for id := range m.votes {
		ids = append(ids, id)
	}
	return ids, nil
}

func (m *mockVoteRepo) CountCouncilMembers(_ context.Context) (int64, error) {
	return m.councilCount, nil
}

var votingFixedNow = time.Date(2026, 3, 1, 14, 0, 0, 0, time.UTC)

// --- CastVote ---

func TestVotingService_CastVote_Success(t *testing.T) {
	repo := newMockVoteRepo()
	svc := NewVotingService(repo, func() time.Time { return votingFixedNow })

	summary, err := svc.CastVote(context.Background(), "promote:user-1", "council-1", domain.VoteApprove)
	if err != nil {
		t.Fatalf("CastVote() unexpected error: %v", err)
	}
	if summary.ProposalID != "promote:user-1" {
		t.Errorf("ProposalID = %q, want %q", summary.ProposalID, "promote:user-1")
	}
	if summary.ApproveCount != 1 {
		t.Errorf("ApproveCount = %d, want 1", summary.ApproveCount)
	}
	if summary.Status != domain.ProposalPending {
		t.Errorf("Status = %q, want %q", summary.Status, domain.ProposalPending)
	}
}

func TestVotingService_CastVote_DuplicateVote(t *testing.T) {
	repo := newMockVoteRepo()
	svc := NewVotingService(repo, func() time.Time { return votingFixedNow })

	_, err := svc.CastVote(context.Background(), "promote:user-1", "council-1", domain.VoteApprove)
	if err != nil {
		t.Fatalf("first CastVote() unexpected error: %v", err)
	}

	_, err = svc.CastVote(context.Background(), "promote:user-1", "council-1", domain.VoteApprove)
	if err == nil {
		t.Fatal("second CastVote() expected error, got nil")
	}
}

func TestVotingService_CastVote_MajorityApproves(t *testing.T) {
	repo := newMockVoteRepo()
	repo.councilCount = 3
	svc := NewVotingService(repo, func() time.Time { return votingFixedNow })

	svc.CastVote(context.Background(), "promote:user-1", "council-1", domain.VoteApprove)
	summary, err := svc.CastVote(context.Background(), "promote:user-1", "council-2", domain.VoteApprove)
	if err != nil {
		t.Fatalf("CastVote() unexpected error: %v", err)
	}
	if summary.Status != domain.ProposalApproved {
		t.Errorf("Status = %q, want %q", summary.Status, domain.ProposalApproved)
	}
}

func TestVotingService_CastVote_MajorityRejects(t *testing.T) {
	repo := newMockVoteRepo()
	repo.councilCount = 3
	svc := NewVotingService(repo, func() time.Time { return votingFixedNow })

	svc.CastVote(context.Background(), "promote:user-1", "council-1", domain.VoteReject)
	summary, err := svc.CastVote(context.Background(), "promote:user-1", "council-2", domain.VoteReject)
	if err != nil {
		t.Fatalf("CastVote() unexpected error: %v", err)
	}
	if summary.Status != domain.ProposalRejected {
		t.Errorf("Status = %q, want %q", summary.Status, domain.ProposalRejected)
	}
}

func TestVotingService_CastVote_EmptyProposalID(t *testing.T) {
	repo := newMockVoteRepo()
	svc := NewVotingService(repo, func() time.Time { return votingFixedNow })

	_, err := svc.CastVote(context.Background(), "", "council-1", domain.VoteApprove)
	if err == nil {
		t.Fatal("CastVote() expected error for empty proposal_id, got nil")
	}
}

func TestVotingService_CastVote_InvalidVoteChoice(t *testing.T) {
	repo := newMockVoteRepo()
	svc := NewVotingService(repo, func() time.Time { return votingFixedNow })

	_, err := svc.CastVote(context.Background(), "promote:user-1", "council-1", domain.VoteChoice("abstain"))
	if err == nil {
		t.Fatal("CastVote() expected error for invalid vote choice, got nil")
	}
}

// --- ListPendingProposals ---

func TestVotingService_ListPendingProposals_Success(t *testing.T) {
	repo := newMockVoteRepo()
	svc := NewVotingService(repo, func() time.Time { return votingFixedNow })

	svc.CastVote(context.Background(), "promote:user-1", "council-1", domain.VoteApprove)

	proposals, err := svc.ListPendingProposals(context.Background())
	if err != nil {
		t.Fatalf("ListPendingProposals() unexpected error: %v", err)
	}
	if len(proposals) != 1 {
		t.Fatalf("ListPendingProposals() returned %d proposals, want 1", len(proposals))
	}
	if proposals[0].ProposalID != "promote:user-1" {
		t.Errorf("proposals[0].ProposalID = %q, want %q", proposals[0].ProposalID, "promote:user-1")
	}
	if proposals[0].TotalCouncil != 3 {
		t.Errorf("proposals[0].TotalCouncil = %d, want 3", proposals[0].TotalCouncil)
	}
}

func TestVotingService_ListPendingProposals_Empty(t *testing.T) {
	repo := newMockVoteRepo()
	svc := NewVotingService(repo, func() time.Time { return votingFixedNow })

	proposals, err := svc.ListPendingProposals(context.Background())
	if err != nil {
		t.Fatalf("ListPendingProposals() unexpected error: %v", err)
	}
	if proposals == nil {
		t.Fatal("ListPendingProposals() returned nil, want empty slice")
	}
	if len(proposals) != 0 {
		t.Errorf("ListPendingProposals() returned %d proposals, want 0", len(proposals))
	}
}

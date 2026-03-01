package postgres

import (
	"context"
	"errors"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type VoteRepo struct {
	q *Queries
}

func NewVoteRepo(q *Queries) *VoteRepo {
	return &VoteRepo{q: q}
}

func (r *VoteRepo) CreateVote(ctx context.Context, vote *domain.CouncilVote) error {
	_, err := r.q.CreateCouncilVote(ctx, CreateCouncilVoteParams{
		ID:         vote.ID,
		ProposalID: vote.ProposalID,
		VoterID:    vote.VoterID,
		Vote:       string(vote.Vote),
		CreatedAt:  pgtype.Timestamptz{Time: vote.CreatedAt, Valid: true},
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return service.ErrValidation
		}
		return err
	}
	return nil
}

func (r *VoteRepo) GetVoteByProposalAndVoter(ctx context.Context, proposalID, voterID string) (*domain.CouncilVote, error) {
	row, err := r.q.GetVoteByProposalAndVoter(ctx, GetVoteByProposalAndVoterParams{
		ProposalID: proposalID,
		VoterID:    voterID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, service.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return councilVoteFromRow(row), nil
}

func (r *VoteRepo) ListVotesByProposal(ctx context.Context, proposalID string) ([]domain.CouncilVote, error) {
	rows, err := r.q.ListVotesByProposal(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	votes := make([]domain.CouncilVote, len(rows))
	for i, row := range rows {
		votes[i] = *councilVoteFromRow(row)
	}
	return votes, nil
}

func (r *VoteRepo) CountVotes(ctx context.Context, proposalID string, vote domain.VoteChoice) (int64, error) {
	return r.q.CountVotesByProposalAndVote(ctx, CountVotesByProposalAndVoteParams{
		ProposalID: proposalID,
		Vote:       string(vote),
	})
}

func (r *VoteRepo) ListOpenProposalIDs(ctx context.Context) ([]string, error) {
	return r.q.ListDistinctOpenProposals(ctx)
}

func (r *VoteRepo) CountCouncilMembers(ctx context.Context) (int64, error) {
	return r.q.CountCouncilMembers(ctx)
}

func councilVoteFromRow(row CouncilVote) *domain.CouncilVote {
	return &domain.CouncilVote{
		ID:         row.ID,
		ProposalID: row.ProposalID,
		VoterID:    row.VoterID,
		Vote:       domain.VoteChoice(row.Vote),
		CreatedAt:  row.CreatedAt.Time,
	}
}

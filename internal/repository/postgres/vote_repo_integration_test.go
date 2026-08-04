//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/fireynis/the-bell/internal/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Council votes decide promotions, so the tally has to be right and a council
// member must not be able to vote twice. Both properties are enforced by the
// schema (a unique index on proposal_id, voter_id) rather than by Go, so they
// are checked against a real database.

var voteSeq int

func castVote(t *testing.T, pool *pgxpool.Pool, proposalID, voterID string, choice domain.VoteChoice) *domain.CouncilVote {
	t.Helper()

	voteSeq++
	vote := &domain.CouncilVote{
		ID:         fmt.Sprintf("vote-%d", voteSeq),
		ProposalID: proposalID,
		VoterID:    voterID,
		Vote:       choice,
		CreatedAt:  time.Now(),
	}
	if err := postgres.NewVoteRepo(postgres.New(pool)).CreateVote(context.Background(), vote); err != nil {
		t.Fatalf("casting vote %s: %v", vote.ID, err)
	}
	return vote
}

func councilMember(t *testing.T, pool *pgxpool.Pool, suffix string) *domain.User {
	t.Helper()
	return testsupport.TestUser(t, pool, testsupport.UniqueKratosID("council-"+suffix), domain.RoleCouncil, 90)
}

func TestVoteRepo_CreateAndGet(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewVoteRepo(postgres.New(pool))

	voter := councilMember(t, pool, "a")
	cast := castVote(t, pool, "promote:user-1", voter.ID, domain.VoteApprove)

	got, err := repo.GetVoteByProposalAndVoter(ctx, "promote:user-1", voter.ID)
	if err != nil {
		t.Fatalf("GetVoteByProposalAndVoter: %v", err)
	}
	if got.ID != cast.ID || got.Vote != domain.VoteApprove || got.VoterID != voter.ID {
		t.Errorf("got %+v, want %+v", got, cast)
	}
	if got.CreatedAt.IsZero() {
		t.Error("created_at came back zero")
	}
}

// A council member who has not voted is a normal state — the UI asks before it
// renders the ballot — so it maps to ErrNotFound rather than a raw pgx error.
func TestVoteRepo_GetVoteByProposalAndVoter_NotFound(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewVoteRepo(postgres.New(pool))

	voter := councilMember(t, pool, "a")
	castVote(t, pool, "promote:user-1", voter.ID, domain.VoteApprove)

	tests := []struct {
		name                string
		proposalID, voterID string
	}{
		{"proposal this voter has not voted on", "promote:user-2", voter.ID},
		{"a voter who has not voted", "promote:user-1", "no-such-voter"},
		{"neither exists", "promote:nobody", "no-such-voter"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repo.GetVoteByProposalAndVoter(ctx, tt.proposalID, tt.voterID)
			if !errors.Is(err, service.ErrNotFound) {
				t.Errorf("err = %v, want service.ErrNotFound", err)
			}
			if got != nil {
				t.Errorf("got %+v, want nil", got)
			}
		})
	}
}

// Voting twice on the same proposal is ordinary user behaviour — a double-click
// or a stale tab — so the unique-index violation must arrive as ErrValidation
// and not as a 500.
func TestVoteRepo_CreateVote_DuplicateIsValidationError(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewVoteRepo(postgres.New(pool))

	voter := councilMember(t, pool, "a")
	castVote(t, pool, "promote:user-1", voter.ID, domain.VoteApprove)

	// Same proposal and voter, new row id, and even the opposite choice: the
	// index is on (proposal_id, voter_id), so this is still a duplicate.
	second := &domain.CouncilVote{
		ID:         "vote-duplicate",
		ProposalID: "promote:user-1",
		VoterID:    voter.ID,
		Vote:       domain.VoteReject,
		CreatedAt:  time.Now(),
	}
	err := repo.CreateVote(ctx, second)
	if !errors.Is(err, service.ErrValidation) {
		t.Fatalf("err = %v, want service.ErrValidation", err)
	}

	// The original vote stands; the duplicate changed nothing.
	votes, err := repo.ListVotesByProposal(ctx, "promote:user-1")
	if err != nil {
		t.Fatalf("ListVotesByProposal: %v", err)
	}
	if len(votes) != 1 {
		t.Fatalf("got %d votes, want 1", len(votes))
	}
	if votes[0].Vote != domain.VoteApprove {
		t.Errorf("vote = %q, want the original %q", votes[0].Vote, domain.VoteApprove)
	}
}

// The same voter on a different proposal is not a duplicate.
func TestVoteRepo_CreateVote_SameVoterDifferentProposal(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewVoteRepo(postgres.New(pool))

	voter := councilMember(t, pool, "a")
	castVote(t, pool, "promote:user-1", voter.ID, domain.VoteApprove)
	castVote(t, pool, "promote:user-2", voter.ID, domain.VoteReject)

	for proposal, want := range map[string]domain.VoteChoice{
		"promote:user-1": domain.VoteApprove,
		"promote:user-2": domain.VoteReject,
	} {
		got, err := repo.GetVoteByProposalAndVoter(ctx, proposal, voter.ID)
		if err != nil {
			t.Fatalf("GetVoteByProposalAndVoter(%s): %v", proposal, err)
		}
		if got.Vote != want {
			t.Errorf("%s: vote = %q, want %q", proposal, got.Vote, want)
		}
	}
}

func TestVoteRepo_ListVotesByProposal(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewVoteRepo(postgres.New(pool))

	a := councilMember(t, pool, "a")
	b := councilMember(t, pool, "b")
	c := councilMember(t, pool, "c")

	castVote(t, pool, "promote:user-1", a.ID, domain.VoteApprove)
	castVote(t, pool, "promote:user-1", b.ID, domain.VoteReject)
	// A vote on a different proposal must not appear.
	castVote(t, pool, "promote:user-2", c.ID, domain.VoteApprove)

	votes, err := repo.ListVotesByProposal(ctx, "promote:user-1")
	if err != nil {
		t.Fatalf("ListVotesByProposal: %v", err)
	}
	if len(votes) != 2 {
		t.Fatalf("got %d votes, want 2", len(votes))
	}
	for _, v := range votes {
		if v.ProposalID != "promote:user-1" {
			t.Errorf("vote %s belongs to proposal %s", v.ID, v.ProposalID)
		}
		if v.VoterID == c.ID {
			t.Error("a vote from the other proposal leaked in")
		}
	}

	empty, err := repo.ListVotesByProposal(ctx, "promote:nobody")
	if err != nil {
		t.Fatalf("ListVotesByProposal: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("got %d votes for an unknown proposal, want 0", len(empty))
	}
}

// The tally is what decides a promotion, so approve and reject must be counted
// separately and scoped to their own proposal.
func TestVoteRepo_CountVotes(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewVoteRepo(postgres.New(pool))

	a := councilMember(t, pool, "a")
	b := councilMember(t, pool, "b")
	c := councilMember(t, pool, "c")

	castVote(t, pool, "promote:user-1", a.ID, domain.VoteApprove)
	castVote(t, pool, "promote:user-1", b.ID, domain.VoteApprove)
	castVote(t, pool, "promote:user-1", c.ID, domain.VoteReject)
	castVote(t, pool, "promote:user-2", a.ID, domain.VoteReject)

	tests := []struct {
		proposal string
		choice   domain.VoteChoice
		want     int64
	}{
		{"promote:user-1", domain.VoteApprove, 2},
		{"promote:user-1", domain.VoteReject, 1},
		{"promote:user-2", domain.VoteApprove, 0},
		{"promote:user-2", domain.VoteReject, 1},
		{"promote:nobody", domain.VoteApprove, 0},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%s", tt.proposal, tt.choice), func(t *testing.T) {
			got, err := repo.CountVotes(ctx, tt.proposal, tt.choice)
			if err != nil {
				t.Fatalf("CountVotes: %v", err)
			}
			if got != tt.want {
				t.Errorf("count = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestVoteRepo_ListOpenProposalIDs(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewVoteRepo(postgres.New(pool))

	empty, err := repo.ListOpenProposalIDs(ctx)
	if err != nil {
		t.Fatalf("ListOpenProposalIDs: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("got %v before any votes, want none", empty)
	}

	a := councilMember(t, pool, "a")
	b := councilMember(t, pool, "b")
	// Two votes on the same proposal must collapse to one id.
	castVote(t, pool, "promote:user-1", a.ID, domain.VoteApprove)
	castVote(t, pool, "promote:user-1", b.ID, domain.VoteReject)
	castVote(t, pool, "promote:user-2", a.ID, domain.VoteApprove)

	got, err := repo.ListOpenProposalIDs(ctx)
	if err != nil {
		t.Fatalf("ListOpenProposalIDs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %v, want 2 distinct proposals", got)
	}
	// The query orders by proposal_id.
	if got[0] != "promote:user-1" || got[1] != "promote:user-2" {
		t.Errorf("got %v, want [promote:user-1 promote:user-2]", got)
	}
}

// The council size is the denominator of the vote threshold, so it must count
// only active council members — not moderators, and not a resigned member whose
// account was deactivated.
func TestVoteRepo_CountCouncilMembers(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewVoteRepo(postgres.New(pool))
	users := postgres.NewUserRepo(postgres.New(pool))

	if got, err := repo.CountCouncilMembers(ctx); err != nil || got != 0 {
		t.Fatalf("CountCouncilMembers on an empty town = %d, %v; want 0, nil", got, err)
	}

	councilMember(t, pool, "a")
	councilMember(t, pool, "b")
	testsupport.TestUser(t, pool, testsupport.UniqueKratosID("mod"), domain.RoleModerator, 80)
	testsupport.TestUser(t, pool, testsupport.UniqueKratosID("member"), domain.RoleMember, 50)
	testsupport.TestUser(t, pool, testsupport.UniqueKratosID("pending"), domain.RolePending, 50)

	got, err := repo.CountCouncilMembers(ctx)
	if err != nil {
		t.Fatalf("CountCouncilMembers: %v", err)
	}
	if got != 2 {
		t.Fatalf("count = %d, want 2 council members", got)
	}

	resigned := councilMember(t, pool, "resigned")
	if err := users.DeactivateUser(ctx, resigned.ID); err != nil {
		t.Fatalf("DeactivateUser: %v", err)
	}

	got, err = repo.CountCouncilMembers(ctx)
	if err != nil {
		t.Fatalf("CountCouncilMembers: %v", err)
	}
	if got != 2 {
		t.Errorf("count = %d, want a deactivated council member excluded", got)
	}
}

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

// Council votes decide promotions and removals, so the ballot has to be right
// and a council member must not be able to vote twice. Both properties are
// enforced by the schema — a unique index on (proposal_id, voter_id), and since
// 00021 a foreign key from proposal_id to proposals — rather than by Go, so
// they are checked against a real database.
//
// Every proposal id here belongs to a proposal that actually exists. Before
// 00021 these tests voted on strings like "promote:user-1" that referred to
// nothing, which is exactly the state the migration exists to end.

var voteSeq int

// seedProposal creates a real motion to vote on and returns its id.
//
// Each one gets a moderator of its own to be about. That is not incidental: the
// partial unique index allows a single open motion per (type, target), so two
// seeds sharing a target — or both leaving it NULL — would be the same question
// asked twice and the second would be refused.
func seedProposal(t *testing.T, pool *pgxpool.Pool, creator *domain.User) string {
	t.Helper()

	voteSeq++
	id := fmt.Sprintf("proposal-%d-%d", time.Now().UnixNano(), voteSeq)
	target := testsupport.TestUser(t, pool,
		testsupport.UniqueKratosID("proposal-target"), domain.RoleModerator, 80)

	p := &domain.Proposal{
		ID:           id,
		Type:         domain.ProposalCouncilPromotion,
		TargetUserID: target.ID,
		Rationale:    "seeded by a test",
		CreatedBy:    creator.ID,
		Status:       domain.ProposalOpen,
		CreatedAt:    time.Now(),
	}
	if err := postgres.NewProposalRepo(postgres.New(pool)).CreateProposal(context.Background(), p); err != nil {
		t.Fatalf("seeding proposal %s: %v", id, err)
	}
	return id
}

func castVote(t *testing.T, pool *pgxpool.Pool, proposalID, voterID string, choice domain.VoteChoice) *domain.CouncilVote {
	t.Helper()

	voteSeq++
	vote := &domain.CouncilVote{
		ID:         fmt.Sprintf("vote-%d-%d", time.Now().UnixNano(), voteSeq),
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

func TestVoteRepo_CreateAndList(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewVoteRepo(postgres.New(pool))

	voter := councilMember(t, pool, "a")
	proposal := seedProposal(t, pool, voter)
	cast := castVote(t, pool, proposal, voter.ID, domain.VoteApprove)

	votes, err := repo.ListVotesByProposal(ctx, proposal)
	if err != nil {
		t.Fatalf("ListVotesByProposal: %v", err)
	}
	if len(votes) != 1 {
		t.Fatalf("got %d votes, want 1", len(votes))
	}
	got := votes[0]
	if got.ID != cast.ID || got.Vote != domain.VoteApprove || got.VoterID != voter.ID {
		t.Errorf("got %+v, want %+v", got, cast)
	}
	if got.CreatedAt.IsZero() {
		t.Error("created_at came back zero")
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
	proposal := seedProposal(t, pool, voter)
	castVote(t, pool, proposal, voter.ID, domain.VoteApprove)

	// Same proposal and voter, new row id, and even the opposite choice: the
	// index is on (proposal_id, voter_id), so this is still a duplicate.
	second := &domain.CouncilVote{
		ID:         "vote-duplicate",
		ProposalID: proposal,
		VoterID:    voter.ID,
		Vote:       domain.VoteReject,
		CreatedAt:  time.Now(),
	}
	err := repo.CreateVote(ctx, second)
	if !errors.Is(err, service.ErrValidation) {
		t.Fatalf("err = %v, want service.ErrValidation", err)
	}

	// The original vote stands; the duplicate changed nothing.
	votes, err := repo.ListVotesByProposal(ctx, proposal)
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

// A vote on a motion that does not exist is the caller naming something gone,
// not a server fault, so the foreign key added in 00021 must surface as
// ErrNotFound. Before that migration this insert succeeded and the vote sat in
// the table forever, counted by nothing.
func TestVoteRepo_CreateVote_UnknownProposalIsNotFound(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewVoteRepo(postgres.New(pool))

	voter := councilMember(t, pool, "a")

	err := repo.CreateVote(ctx, &domain.CouncilVote{
		ID:         "vote-on-nothing",
		ProposalID: "no-such-proposal",
		VoterID:    voter.ID,
		Vote:       domain.VoteApprove,
		CreatedAt:  time.Now(),
	})
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("err = %v, want service.ErrNotFound", err)
	}
}

// The same voter on a different proposal is not a duplicate.
func TestVoteRepo_CreateVote_SameVoterDifferentProposal(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewVoteRepo(postgres.New(pool))

	voter := councilMember(t, pool, "a")
	first := seedProposal(t, pool, voter)
	second := seedProposal(t, pool, voter)

	castVote(t, pool, first, voter.ID, domain.VoteApprove)
	castVote(t, pool, second, voter.ID, domain.VoteReject)

	for proposal, want := range map[string]domain.VoteChoice{
		first:  domain.VoteApprove,
		second: domain.VoteReject,
	} {
		votes, err := repo.ListVotesByProposal(ctx, proposal)
		if err != nil {
			t.Fatalf("ListVotesByProposal(%s): %v", proposal, err)
		}
		if len(votes) != 1 {
			t.Fatalf("%s: got %d votes, want 1", proposal, len(votes))
		}
		if votes[0].Vote != want {
			t.Errorf("%s: vote = %q, want %q", proposal, votes[0].Vote, want)
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

	subject := seedProposal(t, pool, a)
	other := seedProposal(t, pool, a)

	castVote(t, pool, subject, a.ID, domain.VoteApprove)
	castVote(t, pool, subject, b.ID, domain.VoteReject)
	// A vote on a different proposal must not appear.
	castVote(t, pool, other, c.ID, domain.VoteApprove)

	votes, err := repo.ListVotesByProposal(ctx, subject)
	if err != nil {
		t.Fatalf("ListVotesByProposal: %v", err)
	}
	if len(votes) != 2 {
		t.Fatalf("got %d votes, want 2", len(votes))
	}
	for _, v := range votes {
		if v.ProposalID != subject {
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

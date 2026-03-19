# Council Voting System Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a council voting system where council members can create proposals, cast votes, and decisions pass by simple majority (>50% of council members).

**Architecture:** New `council_votes` table tracks proposals and individual votes. A `VotingService` in `internal/service/voting.go` depends on a `VoteRepository` and `VotingUserRepository` to manage proposal creation, vote casting, and result tallying. A `VotingHandler` in `internal/handler/voting.go` exposes two endpoints: `POST /api/v1/admin/council/votes` (cast vote on a proposal) and `GET /api/v1/admin/council/votes` (list pending proposals). Proposals are identified by a `proposal_id` (e.g., "promote:user-123", "policy:max-posts-per-day") and each council member gets one vote per proposal. A proposal passes when >50% of active council members vote "approve", and fails when >50% vote "reject" or it becomes mathematically impossible to pass.

**Tech Stack:** Go, chi router, sqlc, pgx/v5, goose migrations

---

### Task 1: Create council_votes migration

**Files:**
- Create: `migrations/00010_create_council_votes.sql`

**Step 1: Write the migration**

Create `migrations/00010_create_council_votes.sql`:

```sql
-- +goose Up
CREATE TABLE council_votes (
    id          TEXT PRIMARY KEY,
    proposal_id TEXT NOT NULL,
    voter_id    TEXT NOT NULL REFERENCES users(id),
    vote        TEXT NOT NULL CHECK (vote IN ('approve', 'reject')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_council_votes_proposal_voter ON council_votes(proposal_id, voter_id);
CREATE INDEX idx_council_votes_proposal ON council_votes(proposal_id);

-- +goose Down
DROP TABLE IF EXISTS council_votes;
```

**Step 2: Verify migration syntax by building**

Run: `go build ./...`
Expected: PASS (goose embeds migrations at build time)

**Step 3: Commit**

```bash
git add migrations/00010_create_council_votes.sql
git commit -m "feat: add council_votes table migration"
```

---

### Task 2: Add sqlc queries for council votes

**Files:**
- Create: `queries/council_votes.sql`
- Modify: `queries/users.sql` (add CountCouncilMembers query)

**Step 1: Write the council_votes SQL queries**

Create `queries/council_votes.sql`:

```sql
-- name: CreateCouncilVote :one
INSERT INTO council_votes (id, proposal_id, voter_id, vote, created_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetVoteByProposalAndVoter :one
SELECT * FROM council_votes
WHERE proposal_id = $1 AND voter_id = $2;

-- name: ListVotesByProposal :many
SELECT * FROM council_votes
WHERE proposal_id = $1
ORDER BY created_at ASC;

-- name: CountVotesByProposalAndVote :one
SELECT COUNT(*) FROM council_votes
WHERE proposal_id = $1 AND vote = $2;

-- name: ListDistinctOpenProposals :many
SELECT DISTINCT proposal_id FROM council_votes
ORDER BY proposal_id;
```

**Step 2: Add CountCouncilMembers query to users.sql**

Add to end of `queries/users.sql`:

```sql
-- name: CountCouncilMembers :one
SELECT COUNT(*) FROM users
WHERE role = 'council' AND is_active = TRUE;
```

**Step 3: Run sqlc generate**

Run: `sqlc generate`
Expected: New file `internal/repository/postgres/council_votes.sql.go` generated, and `users.sql.go` updated.

**Step 4: Verify build compiles**

Run: `go build ./...`
Expected: PASS

**Step 5: Commit**

```bash
git add queries/council_votes.sql queries/users.sql internal/repository/postgres/council_votes.sql.go internal/repository/postgres/users.sql.go internal/repository/postgres/models.go
git commit -m "feat: add sqlc queries for council votes and council member count"
```

---

### Task 3: Add domain types for voting

**Files:**
- Create: `internal/domain/vote.go`

**Step 1: Write the failing test**

No test needed for pure value types — these will be tested via the service layer.

**Step 2: Write the domain types**

Create `internal/domain/vote.go`:

```go
package domain

import "time"

type VoteChoice string

const (
	VoteApprove VoteChoice = "approve"
	VoteReject  VoteChoice = "reject"
)

type CouncilVote struct {
	ID         string
	ProposalID string
	VoterID    string
	Vote       VoteChoice
	CreatedAt  time.Time
}

type ProposalStatus string

const (
	ProposalPending  ProposalStatus = "pending"
	ProposalApproved ProposalStatus = "approved"
	ProposalRejected ProposalStatus = "rejected"
)

type ProposalSummary struct {
	ProposalID   string
	ApproveCount int64
	RejectCount  int64
	TotalCouncil int64
	Status       ProposalStatus
	Votes        []CouncilVote
}
```

**Step 3: Verify build compiles**

Run: `go build ./...`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/domain/vote.go
git commit -m "feat: add domain types for council voting"
```

---

### Task 4: Add VoteRepo (repository adapter)

**Files:**
- Create: `internal/repository/postgres/vote_repo.go`

**Step 1: Write the repository adapter**

Create `internal/repository/postgres/vote_repo.go`:

```go
package postgres

import (
	"context"
	"errors"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
		CreatedAt:  timestamptz(vote.CreatedAt),
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
	return voteFromRow(row), nil
}

func (r *VoteRepo) ListVotesByProposal(ctx context.Context, proposalID string) ([]domain.CouncilVote, error) {
	rows, err := r.q.ListVotesByProposal(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	votes := make([]domain.CouncilVote, len(rows))
	for i, row := range rows {
		votes[i] = *voteFromRow(row)
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

func voteFromRow(row CouncilVote) *domain.CouncilVote {
	return &domain.CouncilVote{
		ID:         row.ID,
		ProposalID: row.ProposalID,
		VoterID:    row.VoterID,
		Vote:       domain.VoteChoice(row.Vote),
		CreatedAt:  row.CreatedAt.Time,
	}
}
```

Note: The `timestamptz` helper may need to be added if it doesn't already exist. Check if `user_repo.go` uses `pgtype.Timestamptz{Time: t, Valid: true}` inline — if so, do the same inline here instead:

```go
// Replace timestamptz(vote.CreatedAt) with:
pgtype.Timestamptz{Time: vote.CreatedAt, Valid: true}
```

**Step 2: Verify build compiles**

Run: `go build ./...`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/repository/postgres/vote_repo.go
git commit -m "feat: add VoteRepo adapter for council votes"
```

---

### Task 5: Write VotingService with tests (TDD)

**Files:**
- Create: `internal/service/voting.go`
- Create: `internal/service/voting_test.go`

**Step 1: Write the failing tests**

Create `internal/service/voting_test.go`:

```go
package service

import (
	"context"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

type mockVoteRepo struct {
	votes          map[string][]domain.CouncilVote // proposalID -> votes
	councilCount   int64
	createErr      error
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
	// Check for duplicate
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

	// Cast a vote to create a proposal
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
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/service/ -run TestVotingService -v`
Expected: FAIL — `NewVotingService` not defined

**Step 3: Write the VotingService implementation**

Create `internal/service/voting.go`:

```go
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
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/service/ -run TestVotingService -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add internal/service/voting.go internal/service/voting_test.go
git commit -m "feat: add VotingService with CastVote and ListPendingProposals"
```

---

### Task 6: Write VotingHandler with tests (TDD)

**Files:**
- Create: `internal/handler/voting.go`
- Create: `internal/handler/voting_test.go`

**Step 1: Write the failing tests**

Create `internal/handler/voting_test.go`:

```go
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

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
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/handler/ -run TestVotingHandler -v`
Expected: FAIL — `NewVotingHandler` not defined

**Step 3: Write the VotingHandler implementation**

Create `internal/handler/voting.go`:

```go
package handler

import (
	"context"
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

	if req.ProposalID == "" {
		Error(w, http.StatusBadRequest, "proposal_id is required")
		return
	}
	if req.Vote != "approve" && req.Vote != "reject" {
		Error(w, http.StatusBadRequest, "vote must be 'approve' or 'reject'")
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
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/handler/ -run TestVotingHandler -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add internal/handler/voting.go internal/handler/voting_test.go
git commit -m "feat: add VotingHandler with CastVote and ListPending endpoints"
```

---

### Task 7: Wire VotingService into server

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/routes.go`
- Modify: `cmd/bell/main.go`

**Step 1: Add votingService field and WithVotingService option**

In `internal/server/server.go`, add a `votingService` field to the `Server` struct:

```go
// Add field to Server struct:
votingService *service.VotingService

// Add option function:
func WithVotingService(vs *service.VotingService) Option {
	return func(s *Server) { s.votingService = vs }
}
```

**Step 2: Add council routes to routes.go**

In `internal/server/routes.go`, add the council vote routes after the approval routes block:

```go
if s.votingService != nil {
	vh := handler.NewVotingHandler(s.votingService)
	r.Route("/api/v1/admin/council/votes", func(r chi.Router) {
		if s.authMiddleware != nil {
			r.Use(s.authMiddleware)
		}
		r.Use(middleware.RequireActive)
		r.Use(middleware.RequireRole(domain.RoleCouncil))
		r.Post("/", vh.CastVote)
		r.Get("/", vh.ListPending)
	})
}
```

**Step 3: Wire VotingService in main.go**

In `cmd/bell/main.go`, in the `runServe` function, add after the `approvalSvc` creation:

```go
voteRepo := postgres.NewVoteRepo(queries)
votingSvc := service.NewVotingService(voteRepo, nil)
```

And add `server.WithVotingService(votingSvc)` to the `server.New()` call.

**Step 4: Verify everything compiles**

Run: `go build ./...`
Expected: PASS

**Step 5: Run all tests**

Run: `go test ./...`
Expected: ALL PASS

**Step 6: Commit**

```bash
git add internal/server/server.go internal/server/routes.go cmd/bell/main.go
git commit -m "feat: wire VotingService into server with council routes"
```

---

### Task 8: Add JSON tags to domain types

**Files:**
- Modify: `internal/domain/vote.go`

**Step 1: Verify JSON serialization works in handler tests**

Check that `domain.ProposalSummary` serializes correctly. Add JSON tags if needed:

```go
type CouncilVote struct {
	ID         string     `json:"id"`
	ProposalID string     `json:"proposal_id"`
	VoterID    string     `json:"voter_id"`
	Vote       VoteChoice `json:"vote"`
	CreatedAt  time.Time  `json:"created_at"`
}

type ProposalSummary struct {
	ProposalID   string         `json:"proposal_id"`
	ApproveCount int64          `json:"approve_count"`
	RejectCount  int64          `json:"reject_count"`
	TotalCouncil int64          `json:"total_council"`
	Status       ProposalStatus `json:"status"`
	Votes        []CouncilVote  `json:"votes"`
}
```

Note: Check the existing `domain/user.go` for whether it uses JSON tags. If not, follow the same convention. The `User` struct in `domain/user.go` does NOT have JSON tags, but sqlc-generated models DO. Since `ProposalSummary` will be directly returned from handlers, JSON tags are needed for predictable field names.

**Step 2: Run all tests**

Run: `go test ./...`
Expected: ALL PASS

**Step 3: Commit**

```bash
git add internal/domain/vote.go
git commit -m "feat: add JSON tags to domain vote types for API serialization"
```

---

## Edge Cases & Design Notes

1. **Duplicate votes**: The unique index `(proposal_id, voter_id)` prevents a council member from voting twice on the same proposal. The repo maps the Postgres unique violation (23505) to `ErrValidation`.

2. **Proposal identity**: Proposals are identified by string IDs like `"promote:user-123"` or `"policy:max-posts-per-day"`. This is flexible and avoids needing a separate proposals table. The caller (future admin dashboard) determines the proposal ID format.

3. **Majority calculation**: `majority = totalCouncil/2 + 1` (integer division). For 3 council members, majority is 2. For 5, it's 3. This means >50% is required, not just >=50%.

4. **No proposal lifecycle table**: Proposals are implicitly created when the first vote is cast, and resolved when they reach majority. There's no explicit "create proposal" step — this keeps the initial implementation simple. A future iteration could add a proposals table with metadata (title, description, deadline).

5. **ListOpenProposalIDs returns all proposals**: Currently it returns all distinct proposal IDs. Once proposals start resolving, you may want to filter out resolved ones. For now, the caller (admin dashboard) can filter by `Status` field in the response.

6. **Council count at vote time**: The council member count is read at vote time and at list time. If council membership changes between votes, the majority threshold adjusts accordingly. This is intentional — it reflects the current state of governance.

7. **No vote changes**: Once cast, a vote cannot be changed. A future iteration could allow vote updates before the proposal resolves.

## Test Strategy

- **Service tests (10 tests)**: Cover CastVote (success, duplicate, majority approve, majority reject, empty proposal ID, invalid choice) and ListPendingProposals (success, empty).
- **Handler tests (7 tests)**: Cover CastVote (success, unauthorized, bad request, validation error) and ListPending (success, empty, service error).
- **All tests use in-memory mocks**: No database required. Mock repos simulate data behavior.
- **Run**: `go test ./internal/service/ -run TestVotingService -v` and `go test ./internal/handler/ -run TestVotingHandler -v`

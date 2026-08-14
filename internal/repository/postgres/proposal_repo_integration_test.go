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

// The proposals table carries three constraints the Go code relies on and does
// not itself enforce: a CHECK on type, a CHECK on status, and a PARTIAL UNIQUE
// index that allows one open motion per (type, target). All three are checked
// here against a real database, because a fake that agreed with the service by
// construction would prove nothing about any of them.

var proposalSeq int

func newProposal(creator *domain.User, t domain.ProposalType, target *domain.User) *domain.Proposal {
	proposalSeq++
	p := &domain.Proposal{
		ID:        fmt.Sprintf("prop-%d-%d", time.Now().UnixNano(), proposalSeq),
		Type:      t,
		Rationale: "because the town needs it",
		CreatedBy: creator.ID,
		Status:    domain.ProposalOpen,
		CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	if target != nil {
		p.TargetUserID = target.ID
	}
	return p
}

func proposalRepo(pool *pgxpool.Pool) *postgres.ProposalRepo {
	return postgres.NewProposalRepo(postgres.New(pool))
}

func TestProposalRepo_CreateAndGet(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := proposalRepo(pool)

	creator := councilMember(t, pool, "creator")
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("mod"), domain.RoleModerator, 80)

	p := newProposal(creator, domain.ProposalCouncilPromotion, target)
	if err := repo.CreateProposal(ctx, p); err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	got, err := repo.GetProposal(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetProposal: %v", err)
	}
	if got.Type != domain.ProposalCouncilPromotion || got.TargetUserID != target.ID {
		t.Errorf("got %+v, want a promotion targeting %s", got, target.ID)
	}
	if got.CreatedBy != creator.ID || got.Rationale != p.Rationale {
		t.Errorf("got %+v, want it created by %s", got, creator.ID)
	}
	if got.Status != domain.ProposalOpen {
		t.Errorf("status = %q, want open", got.Status)
	}
	if got.DecidedAt != nil {
		t.Errorf("decided_at = %v on a new motion, want nil", got.DecidedAt)
	}
}

// A motion about the town has no target. The column is nullable and the domain
// spells absence as the empty string, so the round trip is where the two
// spellings have to agree.
func TestProposalRepo_TownWideProposalRoundTripsWithNoTarget(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := proposalRepo(pool)

	creator := councilMember(t, pool, "creator")
	p := newProposal(creator, domain.ProposalBootstrapReentry, nil)

	if err := repo.CreateProposal(ctx, p); err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	got, err := repo.GetProposal(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetProposal: %v", err)
	}
	if got.TargetUserID != "" {
		t.Errorf("target = %q, want the empty string", got.TargetUserID)
	}

	// And the column really is NULL, not the empty string, which is what makes
	// the foreign key satisfiable.
	var isNull bool
	if err := pool.QueryRow(ctx,
		`SELECT target_user_id IS NULL FROM proposals WHERE id = $1`, p.ID).Scan(&isNull); err != nil {
		t.Fatalf("reading target_user_id: %v", err)
	}
	if !isNull {
		t.Error("target_user_id was stored as the empty string rather than NULL")
	}
}

func TestProposalRepo_GetProposal_UnknownIsNotFound(t *testing.T) {
	pool := testsupport.TestDB(t)

	_, err := proposalRepo(pool).GetProposal(context.Background(), "no-such-proposal")
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("err = %v, want service.ErrNotFound", err)
	}
}

// Two open motions asking the same question would split the council's votes
// between them and neither would reach a majority, so the index refuses the
// second — as a validation error, since it is a councillor repeating a
// colleague rather than a server fault.
func TestProposalRepo_CreateProposal_RefusesASecondOpenMotionOnTheSameQuestion(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := proposalRepo(pool)

	creator := councilMember(t, pool, "creator")
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("mod"), domain.RoleModerator, 80)

	if err := repo.CreateProposal(ctx, newProposal(creator, domain.ProposalCouncilPromotion, target)); err != nil {
		t.Fatalf("first CreateProposal: %v", err)
	}

	err := repo.CreateProposal(ctx, newProposal(creator, domain.ProposalCouncilPromotion, target))
	if !errors.Is(err, service.ErrValidation) {
		t.Fatalf("err = %v, want service.ErrValidation", err)
	}
}

// The case a naive index misses. A unique index treats NULLs as distinct, so
// indexing target_user_id directly would constrain the two targeted types and
// silently exempt bootstrap_reentry — the one type whose target is ALWAYS NULL,
// and therefore the one where every motion would collide. 00021 indexes
// COALESCE(target_user_id, empty string) for exactly this.
func TestProposalRepo_CreateProposal_TownWideMotionsAlsoCollide(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := proposalRepo(pool)

	creator := councilMember(t, pool, "creator")

	if err := repo.CreateProposal(ctx, newProposal(creator, domain.ProposalBootstrapReentry, nil)); err != nil {
		t.Fatalf("first CreateProposal: %v", err)
	}

	err := repo.CreateProposal(ctx, newProposal(creator, domain.ProposalBootstrapReentry, nil))
	if !errors.Is(err, service.ErrValidation) {
		t.Fatalf("err = %v, want service.ErrValidation — two open bootstrap_reentry motions were allowed", err)
	}
}

// Different questions about the same person do not collide, and neither does a
// question the council has already settled: history must not stop the council
// revisiting something next month.
func TestProposalRepo_CreateProposal_OnlyOpenMotionsOnTheSameQuestionCollide(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := proposalRepo(pool)

	creator := councilMember(t, pool, "creator")
	target := councilMember(t, pool, "target")

	promotion := newProposal(creator, domain.ProposalCouncilPromotion, target)
	if err := repo.CreateProposal(ctx, promotion); err != nil {
		t.Fatalf("CreateProposal(promotion): %v", err)
	}
	// A different question about the same person.
	if err := repo.CreateProposal(ctx, newProposal(creator, domain.ProposalCouncilRemoval, target)); err != nil {
		t.Fatalf("CreateProposal(removal): %v", err)
	}

	// Settle the promotion, then ask it again.
	if err := repo.DecideProposal(ctx, promotion.ID, domain.ProposalRejected, time.Now()); err != nil {
		t.Fatalf("DecideProposal: %v", err)
	}
	if err := repo.CreateProposal(ctx, newProposal(creator, domain.ProposalCouncilPromotion, target)); err != nil {
		t.Errorf("re-raising a settled question was refused: %v", err)
	}
}

// The CHECK constraints are the schema's own list of what a motion may be, and
// they must agree with domain.ProposalType and domain.ProposalStatus. A type
// the executor has no branch for would be a motion the council can pass that
// does nothing.
func TestProposalRepo_CreateProposal_RejectsUnknownTypesAndStatuses(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := proposalRepo(pool)

	creator := councilMember(t, pool, "creator")

	badType := newProposal(creator, domain.ProposalType("council_exile"), nil)
	if err := repo.CreateProposal(ctx, badType); err == nil {
		t.Error("the schema accepted an unknown proposal type")
	}

	badStatus := newProposal(creator, domain.ProposalBootstrapReentry, nil)
	badStatus.Status = domain.ProposalStatus("pending")
	if err := repo.CreateProposal(ctx, badStatus); err == nil {
		t.Error("the schema accepted an unknown proposal status")
	}
}

// Every known type and status must be storable, which is the other half of the
// same agreement: a CHECK that is stricter than the domain would fail at
// runtime rather than at review.
func TestProposalRepo_CreateProposal_AcceptsEveryKnownTypeAndStatus(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := proposalRepo(pool)

	creator := councilMember(t, pool, "creator")

	for _, ptype := range []domain.ProposalType{
		domain.ProposalCouncilPromotion,
		domain.ProposalCouncilRemoval,
		domain.ProposalBootstrapReentry,
	} {
		target := (*domain.User)(nil)
		if ptype.RequiresTarget() {
			target = testsupport.TestUser(t, pool, testsupport.UniqueKratosID("t-"+string(ptype)), domain.RoleModerator, 80)
		}
		p := newProposal(creator, ptype, target)
		if err := repo.CreateProposal(ctx, p); err != nil {
			t.Errorf("CreateProposal(%s): %v", ptype, err)
			continue
		}
		for _, status := range []domain.ProposalStatus{domain.ProposalPassed, domain.ProposalRejected} {
			// Re-open so each status can be written in turn.
			if _, err := pool.Exec(ctx, `UPDATE proposals SET status = 'open', decided_at = NULL WHERE id = $1`, p.ID); err != nil {
				t.Fatalf("re-opening %s: %v", p.ID, err)
			}
			if err := repo.DecideProposal(ctx, p.ID, status, time.Now()); err != nil {
				t.Errorf("DecideProposal(%s, %s): %v", ptype, status, err)
			}
		}
	}
}

// A motion naming somebody who does not exist is the caller naming something
// gone, not a server fault.
func TestProposalRepo_CreateProposal_UnknownTargetIsNotFound(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := proposalRepo(pool)

	creator := councilMember(t, pool, "creator")
	p := newProposal(creator, domain.ProposalCouncilPromotion, nil)
	p.TargetUserID = "no-such-user"

	if err := repo.CreateProposal(ctx, p); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("err = %v, want service.ErrNotFound", err)
	}
}

func TestProposalRepo_DecideProposal(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := proposalRepo(pool)

	creator := councilMember(t, pool, "creator")
	p := newProposal(creator, domain.ProposalBootstrapReentry, nil)
	if err := repo.CreateProposal(ctx, p); err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	decided := time.Now().UTC().Truncate(time.Microsecond)
	if err := repo.DecideProposal(ctx, p.ID, domain.ProposalPassed, decided); err != nil {
		t.Fatalf("DecideProposal: %v", err)
	}

	got, err := repo.GetProposal(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetProposal: %v", err)
	}
	if got.Status != domain.ProposalPassed {
		t.Errorf("status = %q, want passed", got.Status)
	}
	if got.DecidedAt == nil || !got.DecidedAt.Equal(decided) {
		t.Errorf("decided_at = %v, want %v", got.DecidedAt, decided)
	}
}

// The query only matches a motion still open, so a second decision cannot
// overwrite the first. That is how two concurrent deciding votes settle it once
// rather than racing to write different outcomes.
func TestProposalRepo_DecideProposal_AlreadyDecidedIsNotFound(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := proposalRepo(pool)

	creator := councilMember(t, pool, "creator")
	p := newProposal(creator, domain.ProposalBootstrapReentry, nil)
	if err := repo.CreateProposal(ctx, p); err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	if err := repo.DecideProposal(ctx, p.ID, domain.ProposalPassed, time.Now()); err != nil {
		t.Fatalf("first DecideProposal: %v", err)
	}

	err := repo.DecideProposal(ctx, p.ID, domain.ProposalRejected, time.Now())
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("err = %v, want service.ErrNotFound", err)
	}

	got, _ := repo.GetProposal(ctx, p.ID)
	if got.Status != domain.ProposalPassed {
		t.Errorf("status = %q, want the first decision to stand", got.Status)
	}
}

func TestProposalRepo_FindOpenProposalByTypeAndTarget(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := proposalRepo(pool)

	creator := councilMember(t, pool, "creator")
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("mod"), domain.RoleModerator, 80)

	promotion := newProposal(creator, domain.ProposalCouncilPromotion, target)
	if err := repo.CreateProposal(ctx, promotion); err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	townWide := newProposal(creator, domain.ProposalBootstrapReentry, nil)
	if err := repo.CreateProposal(ctx, townWide); err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	found, err := repo.FindOpenProposalByTypeAndTarget(ctx, domain.ProposalCouncilPromotion, target.ID)
	if err != nil {
		t.Fatalf("FindOpenProposalByTypeAndTarget: %v", err)
	}
	if found.ID != promotion.ID {
		t.Errorf("found %s, want %s", found.ID, promotion.ID)
	}

	// The NULL-target case has to match itself: `target_user_id = $2` would
	// never find it, because NULL = NULL is unknown.
	found, err = repo.FindOpenProposalByTypeAndTarget(ctx, domain.ProposalBootstrapReentry, "")
	if err != nil {
		t.Fatalf("FindOpenProposalByTypeAndTarget(town-wide): %v", err)
	}
	if found.ID != townWide.ID {
		t.Errorf("found %s, want %s", found.ID, townWide.ID)
	}

	// A question nobody has asked.
	if _, err := repo.FindOpenProposalByTypeAndTarget(ctx, domain.ProposalCouncilRemoval, target.ID); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("err = %v, want service.ErrNotFound", err)
	}

	// And a settled one is no longer open.
	if err := repo.DecideProposal(ctx, promotion.ID, domain.ProposalPassed, time.Now()); err != nil {
		t.Fatalf("DecideProposal: %v", err)
	}
	if _, err := repo.FindOpenProposalByTypeAndTarget(ctx, domain.ProposalCouncilPromotion, target.ID); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("err = %v, want a decided motion to be invisible here", err)
	}
}

// The listings join both names. A LEFT JOIN for the target because a town-wide
// motion has none, and a LEFT JOIN for the proposer so a motion can never
// vanish from the queue because the row it points at became unreadable.
func TestProposalRepo_Listings(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := proposalRepo(pool)

	creator := councilMember(t, pool, "creator")
	target := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("mod"), domain.RoleModerator, 80)

	open := newProposal(creator, domain.ProposalCouncilPromotion, target)
	if err := repo.CreateProposal(ctx, open); err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	townWide := newProposal(creator, domain.ProposalBootstrapReentry, nil)
	if err := repo.CreateProposal(ctx, townWide); err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	settled := newProposal(creator, domain.ProposalCouncilRemoval, target)
	if err := repo.CreateProposal(ctx, settled); err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	if err := repo.DecideProposal(ctx, settled.ID, domain.ProposalRejected, time.Now()); err != nil {
		t.Fatalf("DecideProposal: %v", err)
	}

	openViews, err := repo.ListOpenProposals(ctx)
	if err != nil {
		t.Fatalf("ListOpenProposals: %v", err)
	}
	if len(openViews) != 2 {
		t.Fatalf("%d open proposals, want 2", len(openViews))
	}
	for _, v := range openViews {
		if v.Status != domain.ProposalOpen {
			t.Errorf("%s: status = %q in the open listing", v.ID, v.Status)
		}
		if v.CreatedByDisplayName != creator.DisplayName {
			t.Errorf("%s: proposer name = %q, want %q", v.ID, v.CreatedByDisplayName, creator.DisplayName)
		}
		switch v.ID {
		case open.ID:
			if v.TargetDisplayName != target.DisplayName {
				t.Errorf("target name = %q, want %q", v.TargetDisplayName, target.DisplayName)
			}
		case townWide.ID:
			// The LEFT JOIN's whole point: no target, and the motion is still
			// in the listing rather than dropped by an inner join.
			if v.TargetDisplayName != "" {
				t.Errorf("a town-wide motion carried a target name %q", v.TargetDisplayName)
			}
		}
	}

	decidedViews, err := repo.ListDecidedProposals(ctx, 50)
	if err != nil {
		t.Fatalf("ListDecidedProposals: %v", err)
	}
	if len(decidedViews) != 1 || decidedViews[0].ID != settled.ID {
		t.Fatalf("decided listing = %+v, want just %s", decidedViews, settled.ID)
	}
	if decidedViews[0].Status != domain.ProposalRejected || decidedViews[0].DecidedAt == nil {
		t.Errorf("decided view = %+v, want a rejected motion with a decision time", decidedViews[0])
	}
}

func TestProposalRepo_ListDecidedProposals_RespectsTheLimit(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := proposalRepo(pool)

	creator := councilMember(t, pool, "creator")
	for i := 0; i < 4; i++ {
		p := newProposal(creator, domain.ProposalBootstrapReentry, nil)
		if err := repo.CreateProposal(ctx, p); err != nil {
			t.Fatalf("CreateProposal: %v", err)
		}
		// Settled immediately, so the next one does not collide with it.
		if err := repo.DecideProposal(ctx, p.ID, domain.ProposalRejected, time.Now().Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("DecideProposal: %v", err)
		}
	}

	views, err := repo.ListDecidedProposals(ctx, 2)
	if err != nil {
		t.Fatalf("ListDecidedProposals: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("%d proposals, want the limit of 2", len(views))
	}
	// Newest decision first.
	if views[0].DecidedAt.Before(*views[1].DecidedAt) {
		t.Errorf("decided listing is not newest-first: %v then %v", views[0].DecidedAt, views[1].DecidedAt)
	}
}

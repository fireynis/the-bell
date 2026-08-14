package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

var proposalFixedNow = time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)

// --- fakes ---

// fakeProposalStore is an in-memory ProposalStore. It enforces the one rule the
// schema enforces — a single open motion per (type, target) — because a fake
// that let two exist would let a test pass that the database would reject.
type fakeProposalStore struct {
	proposals map[string]*domain.Proposal
	order     []string

	createErr error
	decideErr error
}

func newFakeProposalStore() *fakeProposalStore {
	return &fakeProposalStore{proposals: make(map[string]*domain.Proposal)}
}

func (f *fakeProposalStore) CreateProposal(_ context.Context, p *domain.Proposal) error {
	if f.createErr != nil {
		return f.createErr
	}
	for _, existing := range f.proposals {
		if existing.Status == domain.ProposalOpen && existing.Type == p.Type && existing.TargetUserID == p.TargetUserID {
			return ErrValidation
		}
	}
	stored := *p
	f.proposals[p.ID] = &stored
	f.order = append(f.order, p.ID)
	return nil
}

func (f *fakeProposalStore) GetProposal(_ context.Context, id string) (*domain.Proposal, error) {
	p, ok := f.proposals[id]
	if !ok {
		return nil, ErrNotFound
	}
	copied := *p
	return &copied, nil
}

func (f *fakeProposalStore) list(open bool) []domain.ProposalView {
	var views []domain.ProposalView
	for _, id := range f.order {
		p := f.proposals[id]
		if (p.Status == domain.ProposalOpen) != open {
			continue
		}
		views = append(views, domain.ProposalView{Proposal: *p})
	}
	return views
}

func (f *fakeProposalStore) ListOpenProposals(_ context.Context) ([]domain.ProposalView, error) {
	return f.list(true), nil
}

func (f *fakeProposalStore) ListDecidedProposals(_ context.Context, limit int) ([]domain.ProposalView, error) {
	views := f.list(false)
	if limit > 0 && len(views) > limit {
		views = views[:limit]
	}
	return views, nil
}

func (f *fakeProposalStore) FindOpenProposalByTypeAndTarget(_ context.Context, t domain.ProposalType, targetID string) (*domain.Proposal, error) {
	for _, p := range f.proposals {
		if p.Status == domain.ProposalOpen && p.Type == t && p.TargetUserID == targetID {
			copied := *p
			return &copied, nil
		}
	}
	return nil, ErrNotFound
}

func (f *fakeProposalStore) DecideProposal(_ context.Context, id string, status domain.ProposalStatus, decidedAt time.Time) error {
	if f.decideErr != nil {
		return f.decideErr
	}
	p, ok := f.proposals[id]
	if !ok || p.Status != domain.ProposalOpen {
		return ErrNotFound
	}
	p.Status = status
	stamped := decidedAt
	p.DecidedAt = &stamped
	return nil
}

// fakeVoteStore is an in-memory VoteRepository. It counts the council off the
// same user store the service reads, so a test that promotes somebody sees the
// electorate change the way it would in production.
type fakeVoteStore struct {
	users     *fakeUserStore
	votes     map[string][]domain.CouncilVote
	createErr error

	// countHook rewrites what CountCouncilMembers reports, per call. It is how
	// a test stages the council changing BETWEEN the tally and the execution —
	// two separate reads in production, and the race executeRemoval's own
	// re-check exists for.
	countHook  func(call int, real int64) int64
	countCalls int
}

func newFakeVoteStore(users *fakeUserStore) *fakeVoteStore {
	return &fakeVoteStore{users: users, votes: make(map[string][]domain.CouncilVote)}
}

func (f *fakeVoteStore) CreateVote(_ context.Context, vote *domain.CouncilVote) error {
	if f.createErr != nil {
		return f.createErr
	}
	for _, v := range f.votes[vote.ProposalID] {
		if v.VoterID == vote.VoterID {
			return ErrValidation
		}
	}
	f.votes[vote.ProposalID] = append(f.votes[vote.ProposalID], *vote)
	return nil
}

func (f *fakeVoteStore) ListVotesByProposal(_ context.Context, proposalID string) ([]domain.CouncilVote, error) {
	return f.votes[proposalID], nil
}

func (f *fakeVoteStore) CountCouncilMembers(_ context.Context) (int64, error) {
	var count int64
	for _, u := range f.users.users {
		if u.Role == domain.RoleCouncil && u.IsActive {
			count++
		}
	}
	f.countCalls++
	if f.countHook != nil {
		return f.countHook(f.countCalls, count), nil
	}
	return count, nil
}

type fakeRoleHistory struct {
	entries []domain.RoleHistory
	err     error
}

func (f *fakeRoleHistory) CreateRoleHistoryEntry(_ context.Context, entry *domain.RoleHistory) error {
	if f.err != nil {
		return f.err
	}
	f.entries = append(f.entries, *entry)
	return nil
}

// --- harness ---

type proposalHarness struct {
	svc       *ProposalService
	proposals *fakeProposalStore
	votes     *fakeVoteStore
	users     *fakeUserStore
	history   *fakeRoleHistory
	config    *mockConfigRepo
}

// newProposalHarness builds a town with a council of the requested size, named
// council-0..council-N, and bootstrap mode already exited.
func newProposalHarness(t *testing.T, councilSize int) *proposalHarness {
	t.Helper()

	users := newFakeUserStore()
	for i := 0; i < councilSize; i++ {
		users.add(&domain.User{
			ID:          councilID(i),
			DisplayName: "Councillor " + string(rune('A'+i)),
			Role:        domain.RoleCouncil,
			IsActive:    true,
		})
	}

	votes := newFakeVoteStore(users)
	history := &fakeRoleHistory{}
	config := newMockConfigRepo()
	config.config["bootstrap_mode"] = "false"
	proposals := newFakeProposalStore()

	return &proposalHarness{
		svc:       NewProposalService(proposals, votes, users, history, config, nil, func() time.Time { return proposalFixedNow }),
		proposals: proposals,
		votes:     votes,
		users:     users,
		history:   history,
		config:    config,
	}
}

func councilID(i int) string { return "council-" + string(rune('0'+i)) }

func (h *proposalHarness) councillor(i int) *domain.User { return h.users.users[councilID(i)] }

func (h *proposalHarness) addUser(id string, role domain.Role, active bool) *domain.User {
	u := &domain.User{ID: id, DisplayName: id, Role: role, IsActive: active}
	h.users.add(u)
	return u
}

// vote casts one council member's vote and fails the test on an unexpected
// error, which keeps the arrange steps of a test from drowning the assertion.
func (h *proposalHarness) vote(t *testing.T, councillorIndex int, proposalID string, approve bool) *domain.ProposalView {
	t.Helper()
	view, err := h.svc.Vote(context.Background(), h.councillor(councillorIndex), proposalID, approve)
	if err != nil {
		t.Fatalf("Vote(council-%d, approve=%v): %v", councillorIndex, approve, err)
	}
	return view
}

// --- Create: who may raise a motion ---

func TestProposalService_Create_RequiresCouncil(t *testing.T) {
	tests := []struct {
		name  string
		actor *domain.User
	}{
		{"nobody at all", nil},
		{"a member", &domain.User{ID: "m", Role: domain.RoleMember, IsActive: true}},
		{"a moderator", &domain.User{ID: "mod", Role: domain.RoleModerator, IsActive: true}},
		{"a pending resident", &domain.User{ID: "p", Role: domain.RolePending, IsActive: true}},
		{"a deactivated council member", &domain.User{ID: "c", Role: domain.RoleCouncil, IsActive: false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newProposalHarness(t, 3)
			_, err := h.svc.Create(context.Background(), tt.actor,
				domain.ProposalBootstrapReentry, "", "because")
			if !errors.Is(err, ErrForbidden) {
				t.Fatalf("error = %v, want ErrForbidden", err)
			}
		})
	}
}

func TestProposalService_Create_RejectsUnknownType(t *testing.T) {
	h := newProposalHarness(t, 3)

	_, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalType("council_exile"), "someone", "because")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}

func TestProposalService_Create_RationaleRules(t *testing.T) {
	tests := []struct {
		name      string
		rationale string
		wantErr   bool
		wantSaved string
	}{
		{"empty", "", true, ""},
		{"only whitespace", "   \n\t ", true, ""},
		{"trimmed", "  she has served the town well  ", false, "she has served the town well"},
		{"at the limit", strings.Repeat("a", maxRationaleLength), false, strings.Repeat("a", maxRationaleLength)},
		{"one over the limit", strings.Repeat("a", maxRationaleLength+1), true, ""},
		// Counted in runes, not bytes: a rationale written in a multi-byte
		// script gets the same allowance as one written in ASCII.
		{"multi-byte at the limit", strings.Repeat("é", maxRationaleLength), false, strings.Repeat("é", maxRationaleLength)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newProposalHarness(t, 3)

			view, err := h.svc.Create(context.Background(), h.councillor(0),
				domain.ProposalBootstrapReentry, "", tt.rationale)

			if tt.wantErr {
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("error = %v, want ErrValidation", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if view.Rationale != tt.wantSaved {
				t.Errorf("rationale = %q, want %q", view.Rationale, tt.wantSaved)
			}
		})
	}
}

// --- Create: council_promotion ---

func TestProposalService_Create_PromotionTargetMustBeAnActiveModerator(t *testing.T) {
	tests := []struct {
		name    string
		role    domain.Role
		active  bool
		wantErr error
	}{
		{"an active moderator", domain.RoleModerator, true, nil},
		{"a member", domain.RoleMember, true, ErrValidation},
		{"a pending resident", domain.RolePending, true, ErrValidation},
		{"someone already on the council", domain.RoleCouncil, true, ErrValidation},
		{"a banned account", domain.RoleBanned, true, ErrValidation},
		{"a deactivated moderator", domain.RoleModerator, false, ErrValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newProposalHarness(t, 3)
			h.addUser("target", tt.role, tt.active)

			_, err := h.svc.Create(context.Background(), h.councillor(0),
				domain.ProposalCouncilPromotion, "target", "they have earned it")

			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Create: %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestProposalService_Create_TargetedTypesRequireATarget(t *testing.T) {
	for _, ptype := range []domain.ProposalType{domain.ProposalCouncilPromotion, domain.ProposalCouncilRemoval} {
		t.Run(string(ptype), func(t *testing.T) {
			h := newProposalHarness(t, 3)

			_, err := h.svc.Create(context.Background(), h.councillor(0), ptype, "  ", "because")
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("error = %v, want ErrValidation", err)
			}
		})
	}
}

func TestProposalService_Create_UnknownTargetIsNotFound(t *testing.T) {
	h := newProposalHarness(t, 3)

	_, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalCouncilPromotion, "nobody", "because")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

// --- Create: council_removal ---

func TestProposalService_Create_RemovalTargetMustBeOnTheCouncil(t *testing.T) {
	h := newProposalHarness(t, 3)
	h.addUser("mod", domain.RoleModerator, true)

	_, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalCouncilRemoval, "mod", "they are not even on the council")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}

// A council of one cannot remove its only member: there would be nobody left to
// approve a resident, change configuration, or vote the council back.
func TestProposalService_Create_RemovalMayNotEmptyTheCouncil(t *testing.T) {
	h := newProposalHarness(t, 1)

	_, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalCouncilRemoval, councilID(0), "I resign")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error = %q, want it to say the council cannot be left empty", err)
	}
}

func TestProposalService_Create_RemovalIsAllowedOnACouncilOfTwo(t *testing.T) {
	h := newProposalHarness(t, 2)

	if _, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalCouncilRemoval, councilID(1), "they have stopped showing up"); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

// --- Create: bootstrap_reentry ---

func TestProposalService_Create_BootstrapReentryTakesNoTarget(t *testing.T) {
	h := newProposalHarness(t, 3)
	h.addUser("someone", domain.RoleMember, true)

	_, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalBootstrapReentry, "someone", "the town has shrunk")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}

func TestProposalService_Create_BootstrapReentryRefusedWhenAlreadyInBootstrap(t *testing.T) {
	h := newProposalHarness(t, 3)
	h.config.config["bootstrap_mode"] = "true"

	_, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalBootstrapReentry, "", "the town has shrunk")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "already") {
		t.Errorf("error = %q, want it to say the town is already in bootstrap mode", err)
	}
}

// The precondition that matters most: above the exit threshold,
// exitBootstrapIfEarned would switch the mode straight back off, so the council
// would have voted for something the system undoes on its own. The refusal has
// to say that, because otherwise the council's only evidence is a motion that
// passed and changed nothing.
func TestProposalService_Create_BootstrapReentryRefusedAtOrAboveTheExitThreshold(t *testing.T) {
	tests := []struct {
		name    string
		members int
		wantErr bool
	}{
		{"one below the threshold", bootstrapExitThreshold - 1, false},
		{"exactly at the threshold", bootstrapExitThreshold, true},
		{"well above the threshold", bootstrapExitThreshold + 5, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newProposalHarness(t, 0)
			for i := 0; i < tt.members; i++ {
				h.addUser("member-"+string(rune('a'+i)), domain.RoleMember, true)
			}
			// A council to raise the motion, kept out of the member count by
			// being added after it is sized... except council members ARE
			// active members, so the harness starts with none and one is added
			// here only when the count allows.
			proposer := &domain.User{ID: "proposer", Role: domain.RoleCouncil, IsActive: true}

			_, err := h.svc.Create(context.Background(), proposer,
				domain.ProposalBootstrapReentry, "", "the town has shrunk")

			if !tt.wantErr {
				if err != nil {
					t.Fatalf("Create: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("error = %v, want ErrValidation", err)
			}
			if !strings.Contains(err.Error(), "undone") {
				t.Errorf("error = %q, want it to explain that re-entry would be undone", err)
			}
		})
	}
}

// --- Create: one open motion per question ---

func TestProposalService_Create_RefusesASecondOpenProposalOnTheSameQuestion(t *testing.T) {
	h := newProposalHarness(t, 3)
	h.addUser("target", domain.RoleModerator, true)

	if _, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalCouncilPromotion, "target", "first"); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	_, err := h.svc.Create(context.Background(), h.councillor(1),
		domain.ProposalCouncilPromotion, "target", "second")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "already") {
		t.Errorf("error = %q, want it to name the open proposal", err)
	}
}

// Two open motions about the same person are only a conflict when they ask the
// same question, and a decided one blocks nothing at all.
func TestProposalService_Create_DifferentQuestionsAndDecidedOnesDoNotCollide(t *testing.T) {
	h := newProposalHarness(t, 3)
	h.addUser("target", domain.RoleModerator, true)

	first, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalCouncilPromotion, "target", "promote them")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// A different question about the town, raised while that one is open.
	if _, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalBootstrapReentry, "", "the town has shrunk"); err != nil {
		t.Fatalf("Create bootstrap re-entry: %v", err)
	}

	// Reject the promotion, then raise it again: history must not block the
	// council from revisiting a question.
	h.vote(t, 0, first.ID, false)
	h.vote(t, 1, first.ID, false)

	if _, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalCouncilPromotion, "target", "let us reconsider"); err != nil {
		t.Fatalf("re-raising a decided question: %v", err)
	}
}

func TestProposalService_Create_ReturnsTheElectorateAndNames(t *testing.T) {
	h := newProposalHarness(t, 5)
	h.addUser("target", domain.RoleModerator, true)

	view, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalCouncilPromotion, "target", "they have earned it")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if view.Status != domain.ProposalOpen {
		t.Errorf("status = %q, want open", view.Status)
	}
	if view.CouncilSize != 5 {
		t.Errorf("council_size = %d, want 5", view.CouncilSize)
	}
	if view.ApproveCount != 0 || view.RejectCount != 0 || view.MyVote != nil {
		t.Errorf("a new motion came back with votes: %+v", view)
	}
	if view.CreatedBy != councilID(0) || view.CreatedByDisplayName != h.councillor(0).DisplayName {
		t.Errorf("proposer = %q/%q, want %q/%q",
			view.CreatedBy, view.CreatedByDisplayName, councilID(0), h.councillor(0).DisplayName)
	}
	if view.TargetUserID != "target" || view.TargetDisplayName != "target" {
		t.Errorf("target = %q/%q, want target/target", view.TargetUserID, view.TargetDisplayName)
	}
	if view.CreatedAt != proposalFixedNow {
		t.Errorf("created_at = %v, want %v", view.CreatedAt, proposalFixedNow)
	}
}

// --- Vote: who may vote, and once ---

func TestProposalService_Vote_RequiresCouncil(t *testing.T) {
	h := newProposalHarness(t, 3)
	view, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalBootstrapReentry, "", "the town has shrunk")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = h.svc.Vote(context.Background(),
		&domain.User{ID: "mod", Role: domain.RoleModerator, IsActive: true}, view.ID, true)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("error = %v, want ErrForbidden", err)
	}
}

func TestProposalService_Vote_UnknownProposalIsNotFound(t *testing.T) {
	h := newProposalHarness(t, 3)

	_, err := h.svc.Vote(context.Background(), h.councillor(0), "no-such-proposal", true)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestProposalService_Vote_RefusesASecondVoteFromTheSameMember(t *testing.T) {
	h := newProposalHarness(t, 5)
	view, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalBootstrapReentry, "", "the town has shrunk")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	h.vote(t, 0, view.ID, true)

	// Same member, opposite choice: changing your mind is still a second vote.
	_, err = h.svc.Vote(context.Background(), h.councillor(0), view.ID, false)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "already voted") {
		t.Errorf("error = %q, want it to say they have already voted", err)
	}
}

func TestProposalService_Vote_RefusesADecidedProposal(t *testing.T) {
	h := newProposalHarness(t, 3)
	view, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalBootstrapReentry, "", "the town has shrunk")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	h.vote(t, 0, view.ID, true)
	h.vote(t, 1, view.ID, true) // 2 of 3 carries it

	_, err = h.svc.Vote(context.Background(), h.councillor(2), view.ID, false)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}

// Nobody votes on whether they keep their own seat.
func TestProposalService_Vote_TargetMayNotVoteOnTheirOwnRemoval(t *testing.T) {
	h := newProposalHarness(t, 3)
	view, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalCouncilRemoval, councilID(2), "they have stopped showing up")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = h.svc.Vote(context.Background(), h.councillor(2), view.ID, false)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("error = %v, want ErrForbidden", err)
	}
	if !strings.Contains(err.Error(), "own removal") {
		t.Errorf("error = %q, want it to name the reason", err)
	}
}

// The target is excluded from the denominator as well as the ballot: on a
// council of three, a removal is decided by two of the other two.
func TestProposalService_Vote_RemovalMajorityExcludesTheTarget(t *testing.T) {
	h := newProposalHarness(t, 3)
	view, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalCouncilRemoval, councilID(2), "they have stopped showing up")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if view.CouncilSize != 2 {
		t.Fatalf("council_size = %d, want the council minus the target (2)", view.CouncilSize)
	}

	first := h.vote(t, 0, view.ID, true)
	if first.Status != domain.ProposalOpen {
		t.Fatalf("status after one of two = %q, want open", first.Status)
	}

	second := h.vote(t, 1, view.ID, true)
	if second.Status != domain.ProposalPassed {
		t.Fatalf("status after two of two = %q, want passed", second.Status)
	}
	if got := h.users.users[councilID(2)].Role; got != domain.RoleMember {
		t.Errorf("removed councillor role = %q, want member", got)
	}
}

// --- Execution ---

func TestProposalService_Vote_PassingPromotionExecutesAndRecordsHistory(t *testing.T) {
	h := newProposalHarness(t, 3)
	h.addUser("target", domain.RoleModerator, true)

	view, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalCouncilPromotion, "target", "they have earned it")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	h.vote(t, 0, view.ID, true)
	final := h.vote(t, 1, view.ID, true)

	if final.Status != domain.ProposalPassed {
		t.Fatalf("status = %q, want passed", final.Status)
	}
	if final.DecidedAt == nil || !final.DecidedAt.Equal(proposalFixedNow) {
		t.Errorf("decided_at = %v, want %v", final.DecidedAt, proposalFixedNow)
	}
	if final.ApproveCount != 2 || final.RejectCount != 0 {
		t.Errorf("tally = %d/%d, want 2/0", final.ApproveCount, final.RejectCount)
	}
	if got := h.users.users["target"].Role; got != domain.RoleCouncil {
		t.Fatalf("target role = %q, want council", got)
	}

	if len(h.history.entries) != 1 {
		t.Fatalf("%d role history entries, want 1", len(h.history.entries))
	}
	entry := h.history.entries[0]
	if entry.UserID != "target" || entry.OldRole != domain.RoleModerator || entry.NewRole != domain.RoleCouncil {
		t.Errorf("entry = %+v, want target moderator->council", entry)
	}
	if !strings.Contains(entry.Reason, view.ID) {
		t.Errorf("reason = %q, want it to name the proposal %q", entry.Reason, view.ID)
	}
}

func TestProposalService_Vote_RejectionChangesNothing(t *testing.T) {
	h := newProposalHarness(t, 3)
	h.addUser("target", domain.RoleModerator, true)

	view, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalCouncilPromotion, "target", "they have earned it")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	h.vote(t, 0, view.ID, false)
	final := h.vote(t, 1, view.ID, false)

	if final.Status != domain.ProposalRejected {
		t.Fatalf("status = %q, want rejected", final.Status)
	}
	if got := h.users.users["target"].Role; got != domain.RoleModerator {
		t.Errorf("target role = %q, want the unchanged moderator", got)
	}
	if len(h.history.entries) != 0 {
		t.Errorf("%d role history entries, want none", len(h.history.entries))
	}
}

// A motion that carries but can no longer be carried out is recorded as
// rejected, not passed. A 'passed' motion that changed nothing would be a lie
// in the council's own record.
func TestProposalService_Vote_PromotionOfSomeoneNoLongerEligibleIsRejected(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(u *domain.User)
	}{
		{"demoted before the vote finished", func(u *domain.User) { u.Role = domain.RoleMember }},
		{"banned before the vote finished", func(u *domain.User) { u.Role = domain.RoleBanned }},
		{"deactivated before the vote finished", func(u *domain.User) { u.IsActive = false }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newProposalHarness(t, 3)
			target := h.addUser("target", domain.RoleModerator, true)

			view, err := h.svc.Create(context.Background(), h.councillor(0),
				domain.ProposalCouncilPromotion, "target", "they have earned it")
			if err != nil {
				t.Fatalf("Create: %v", err)
			}

			h.vote(t, 0, view.ID, true)
			tt.mutate(target)
			final := h.vote(t, 1, view.ID, true)

			if final.Status != domain.ProposalRejected {
				t.Fatalf("status = %q, want rejected", final.Status)
			}
			if target.Role == domain.RoleCouncil {
				t.Error("an ineligible target was promoted anyway")
			}
			if len(h.history.entries) != 0 {
				t.Errorf("%d role history entries, want none", len(h.history.entries))
			}
		})
	}
}

// The council can shrink between the tally that carries a motion and the
// execution that acts on it — two removals raised at once, each legal when
// raised — so emptying the council is refused at execution as well as at
// creation.
//
// The two counts are separate reads in production, so the fake makes the second
// one report a council of one. Staging it through the user store instead is not
// possible: shrinking the council far enough would also shrink the electorate
// to zero, and a motion before an empty electorate never reaches a decision to
// execute in the first place.
func TestProposalService_Vote_RemovalThatWouldEmptyTheCouncilIsRejected(t *testing.T) {
	h := newProposalHarness(t, 3)

	view, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalCouncilRemoval, councilID(2), "they have stopped showing up")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	h.vote(t, 0, view.ID, true)

	// The deciding vote reads the council twice: once to size the electorate,
	// and again inside the execution. Between those two reads the council
	// shrinks to one — only the target left — so carrying the motion would
	// leave the town with no council at all.
	h.votes.countCalls = 0
	h.votes.countHook = func(call int, real int64) int64 {
		if call == 1 {
			return real
		}
		return 1
	}

	final, err := h.svc.Vote(context.Background(), h.councillor(1), view.ID, true)
	if err != nil {
		t.Fatalf("Vote: %v", err)
	}

	if final.Status != domain.ProposalRejected {
		t.Fatalf("status = %q, want rejected", final.Status)
	}
	if got := h.users.users[councilID(2)].Role; got != domain.RoleCouncil {
		t.Errorf("target role = %q, want the unchanged council", got)
	}
	if len(h.history.entries) != 0 {
		t.Errorf("%d role history entries, want none", len(h.history.entries))
	}
}

func TestProposalService_Vote_PassingBootstrapReentryTurnsTheModeBackOn(t *testing.T) {
	h := newProposalHarness(t, 3)

	view, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalBootstrapReentry, "", "half the town moved away")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	h.vote(t, 0, view.ID, true)
	final := h.vote(t, 1, view.ID, true)

	if final.Status != domain.ProposalPassed {
		t.Fatalf("status = %q, want passed", final.Status)
	}
	if got := h.config.config["bootstrap_mode"]; got != "true" {
		t.Errorf("bootstrap_mode = %q, want true", got)
	}
}

// The town can grow past the threshold while the motion is open, and then
// turning the mode on would be undone by the next approval — so it is not
// turned on at all.
func TestProposalService_Vote_BootstrapReentryRejectedWhenTheTownGrewPastTheThreshold(t *testing.T) {
	h := newProposalHarness(t, 3)

	view, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalBootstrapReentry, "", "half the town moved away")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	h.vote(t, 0, view.ID, true)
	for i := 0; i < bootstrapExitThreshold; i++ {
		h.addUser("newcomer-"+string(rune('a'+i)), domain.RoleMember, true)
	}
	final := h.vote(t, 1, view.ID, true)

	if final.Status != domain.ProposalRejected {
		t.Fatalf("status = %q, want rejected", final.Status)
	}
	if got := h.config.config["bootstrap_mode"]; got != "false" {
		t.Errorf("bootstrap_mode = %q, want it left off", got)
	}
}

// --- Self-healing ---

// The vote commits before the execution runs, so an execution failure must
// leave the motion open with its majority intact rather than swallowing the
// error or marking a motion passed that did nothing.
func TestProposalService_Vote_ExecutionFailureLeavesTheProposalOpen(t *testing.T) {
	h := newProposalHarness(t, 3)
	h.addUser("target", domain.RoleModerator, true)

	view, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalCouncilPromotion, "target", "they have earned it")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	h.vote(t, 0, view.ID, true)
	h.users.updateRoleErr = errors.New("database unavailable")

	if _, err := h.svc.Vote(context.Background(), h.councillor(1), view.ID, true); err == nil {
		t.Fatal("Vote returned no error though the execution failed")
	}

	stored, err := h.proposals.GetProposal(context.Background(), view.ID)
	if err != nil {
		t.Fatalf("GetProposal: %v", err)
	}
	if stored.Status != domain.ProposalOpen {
		t.Errorf("status = %q, want it left open for the repair", stored.Status)
	}

	// The vote itself stands, which is what makes the repair possible: the
	// majority is already recorded.
	votes, _ := h.votes.ListVotesByProposal(context.Background(), view.ID)
	if len(votes) != 2 {
		t.Errorf("%d votes recorded, want 2", len(votes))
	}
}

// Listing the open motions is what finishes one that got stuck, in the same way
// requireBootstrap takes a second chance at the bootstrap exit. Without it a
// motion on which every council member has already voted could never be
// re-evaluated, because no further vote can arrive.
func TestProposalService_List_RepairsAProposalWhoseExecutionFailed(t *testing.T) {
	h := newProposalHarness(t, 3)
	h.addUser("target", domain.RoleModerator, true)

	view, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalCouncilPromotion, "target", "they have earned it")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	h.vote(t, 0, view.ID, true)
	h.users.updateRoleErr = errors.New("database unavailable")
	if _, err := h.svc.Vote(context.Background(), h.councillor(1), view.ID, true); err == nil {
		t.Fatal("Vote returned no error though the execution failed")
	}

	// The database comes back.
	h.users.updateRoleErr = nil

	views, err := h.svc.List(context.Background(), h.councillor(2), false, 50)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(views) != 0 {
		t.Errorf("%d open proposals after the repair, want none", len(views))
	}
	stored, err := h.proposals.GetProposal(context.Background(), view.ID)
	if err != nil {
		t.Fatalf("GetProposal: %v", err)
	}
	if stored.Status != domain.ProposalPassed {
		t.Errorf("status = %q, want passed", stored.Status)
	}
	if got := h.users.users["target"].Role; got != domain.RoleCouncil {
		t.Errorf("target role = %q, want council", got)
	}
}

// A repair that fails again must not fail the listing. The council asked to
// read its queue, and a town that cannot mend a stuck motion should still be
// able to see one.
func TestProposalService_List_SurvivesARepairThatFailsAgain(t *testing.T) {
	h := newProposalHarness(t, 3)
	h.addUser("target", domain.RoleModerator, true)

	view, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalCouncilPromotion, "target", "they have earned it")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	h.vote(t, 0, view.ID, true)
	h.users.updateRoleErr = errors.New("database unavailable")
	if _, err := h.svc.Vote(context.Background(), h.councillor(1), view.ID, true); err == nil {
		t.Fatal("Vote returned no error though the execution failed")
	}

	views, err := h.svc.List(context.Background(), h.councillor(2), false, 50)
	if err != nil {
		t.Fatalf("List returned an error though only the repair failed: %v", err)
	}
	if len(views) != 1 || views[0].Status != domain.ProposalOpen {
		t.Fatalf("views = %+v, want the stuck motion still listed as open", views)
	}
}

// Re-running an execution that already committed its role change must not write
// a second role_history row or turn success into a rejection. That is what
// makes the repair safe to run on every listing.
func TestProposalService_List_RepairIsIdempotentOverAnAlreadyPromotedTarget(t *testing.T) {
	h := newProposalHarness(t, 3)
	target := h.addUser("target", domain.RoleModerator, true)

	view, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalCouncilPromotion, "target", "they have earned it")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	h.vote(t, 0, view.ID, true)
	// The role change lands but the status write does not, which is the crash
	// window the repair exists for.
	h.proposals.decideErr = errors.New("database unavailable")
	if _, err := h.svc.Vote(context.Background(), h.councillor(1), view.ID, true); err == nil {
		t.Fatal("Vote returned no error though the decision write failed")
	}
	if target.Role != domain.RoleCouncil {
		t.Fatalf("target role = %q, want the promotion to have committed", target.Role)
	}
	if len(h.history.entries) != 1 {
		t.Fatalf("%d role history entries after the first attempt, want 1", len(h.history.entries))
	}

	h.proposals.decideErr = nil
	if _, err := h.svc.List(context.Background(), h.councillor(2), false, 50); err != nil {
		t.Fatalf("List: %v", err)
	}

	stored, _ := h.proposals.GetProposal(context.Background(), view.ID)
	if stored.Status != domain.ProposalPassed {
		t.Errorf("status = %q, want passed rather than rejected — the target is already council", stored.Status)
	}
	if len(h.history.entries) != 1 {
		t.Errorf("%d role history entries, want the repair not to write a second", len(h.history.entries))
	}
}

// --- List ---

func TestProposalService_List_RequiresCouncil(t *testing.T) {
	h := newProposalHarness(t, 3)

	_, err := h.svc.List(context.Background(),
		&domain.User{ID: "mod", Role: domain.RoleModerator, IsActive: true}, false, 50)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("error = %v, want ErrForbidden", err)
	}
}

// my_vote is per caller, which is the whole reason a view is built per request
// rather than cached.
func TestProposalService_List_MyVoteIsPerCaller(t *testing.T) {
	h := newProposalHarness(t, 5)
	view, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalBootstrapReentry, "", "the town has shrunk")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	h.vote(t, 0, view.ID, true)
	h.vote(t, 1, view.ID, false)

	tests := []struct {
		councillor int
		want       *domain.VoteChoice
	}{
		{0, ptr(domain.VoteApprove)},
		{1, ptr(domain.VoteReject)},
		{2, nil},
	}

	for _, tt := range tests {
		views, err := h.svc.List(context.Background(), h.councillor(tt.councillor), false, 50)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(views) != 1 {
			t.Fatalf("%d open proposals, want 1", len(views))
		}
		got := views[0]
		if got.ApproveCount != 1 || got.RejectCount != 1 {
			t.Errorf("council-%d sees tally %d/%d, want 1/1", tt.councillor, got.ApproveCount, got.RejectCount)
		}
		switch {
		case tt.want == nil && got.MyVote != nil:
			t.Errorf("council-%d: my_vote = %q, want nil", tt.councillor, *got.MyVote)
		case tt.want != nil && (got.MyVote == nil || *got.MyVote != *tt.want):
			t.Errorf("council-%d: my_vote = %v, want %q", tt.councillor, got.MyVote, *tt.want)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func TestProposalService_List_SeparatesOpenFromDecided(t *testing.T) {
	h := newProposalHarness(t, 3)
	h.addUser("target", domain.RoleModerator, true)

	decided, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalCouncilPromotion, "target", "they have earned it")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	h.vote(t, 0, decided.ID, false)
	h.vote(t, 1, decided.ID, false)

	open, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalBootstrapReentry, "", "the town has shrunk")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	openViews, err := h.svc.List(context.Background(), h.councillor(0), false, 50)
	if err != nil {
		t.Fatalf("List(open): %v", err)
	}
	if len(openViews) != 1 || openViews[0].ID != open.ID {
		t.Errorf("open listing = %+v, want just %s", openViews, open.ID)
	}

	decidedViews, err := h.svc.List(context.Background(), h.councillor(0), true, 50)
	if err != nil {
		t.Fatalf("List(decided): %v", err)
	}
	if len(decidedViews) != 1 || decidedViews[0].ID != decided.ID {
		t.Errorf("decided listing = %+v, want just %s", decidedViews, decided.ID)
	}
	if decidedViews[0].Status != domain.ProposalRejected {
		t.Errorf("decided status = %q, want rejected", decidedViews[0].Status)
	}
}

// The electorate travels with each motion, so a page holding a removal and a
// promotion reports two different denominators.
func TestProposalService_List_ElectorateIsPerProposal(t *testing.T) {
	h := newProposalHarness(t, 4)
	h.addUser("target", domain.RoleModerator, true)

	if _, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalCouncilPromotion, "target", "they have earned it"); err != nil {
		t.Fatalf("Create promotion: %v", err)
	}
	if _, err := h.svc.Create(context.Background(), h.councillor(0),
		domain.ProposalCouncilRemoval, councilID(3), "they have stopped showing up"); err != nil {
		t.Fatalf("Create removal: %v", err)
	}

	views, err := h.svc.List(context.Background(), h.councillor(0), false, 50)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("%d open proposals, want 2", len(views))
	}

	for _, v := range views {
		want := int64(4)
		if v.Type == domain.ProposalCouncilRemoval {
			want = 3
		}
		if v.CouncilSize != want {
			t.Errorf("%s: council_size = %d, want %d", v.Type, v.CouncilSize, want)
		}
	}
}

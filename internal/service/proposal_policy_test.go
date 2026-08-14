package service

import (
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
)

func TestSimpleMajority(t *testing.T) {
	tests := []struct {
		electorate int64
		want       int64
	}{
		{0, 0},
		{-3, 0},
		{1, 1},
		{2, 2},
		{3, 2},
		{4, 3},
		{5, 3},
		{6, 4},
		{7, 4},
	}

	for _, tt := range tests {
		if got := simpleMajority(tt.electorate); got != tt.want {
			t.Errorf("simpleMajority(%d) = %d, want %d", tt.electorate, got, tt.want)
		}
	}
}

// A majority must be strictly more than half, so an even electorate cannot
// decide on a tie — 2 of 4 is not enough either way.
func TestSimpleMajority_EvenElectorateNeedsMoreThanHalf(t *testing.T) {
	for _, electorate := range []int64{2, 4, 6, 10} {
		half := electorate / 2
		if got := simpleMajority(electorate); got <= half {
			t.Errorf("simpleMajority(%d) = %d, want more than half (%d)", electorate, got, half)
		}
	}
}

func TestEvaluateProposal(t *testing.T) {
	tests := []struct {
		name       string
		approve    int64
		reject     int64
		electorate int64
		want       domain.ProposalStatus
	}{
		{"no votes yet", 0, 0, 5, domain.ProposalOpen},
		{"approval majority of five", 3, 0, 5, domain.ProposalPassed},
		{"one short of approval", 2, 0, 5, domain.ProposalOpen},
		{"rejection majority of five", 0, 3, 5, domain.ProposalRejected},
		{"one short of rejection", 0, 2, 5, domain.ProposalOpen},
		{"split vote stays open", 2, 2, 5, domain.ProposalOpen},
		{"even electorate tie stays open", 2, 2, 4, domain.ProposalOpen},
		{"even electorate decided", 3, 1, 4, domain.ProposalPassed},
		{"unanimous approval", 5, 0, 5, domain.ProposalPassed},
		{"unanimous rejection", 0, 5, 5, domain.ProposalRejected},
		{"single member council carries alone", 1, 0, 1, domain.ProposalPassed},
		{"empty electorate can never decide", 5, 5, 0, domain.ProposalOpen},
		{"negative electorate is treated as empty", 3, 0, -1, domain.ProposalOpen},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateProposal(tt.approve, tt.reject, tt.electorate)
			if got != tt.want {
				t.Errorf("evaluateProposal(%d, %d, %d) = %q, want %q",
					tt.approve, tt.reject, tt.electorate, got, tt.want)
			}
		})
	}
}

// An empty electorate must not carry a motion: a miscounted or not-yet-seeded
// council would otherwise pass everything with zero votes — including, since
// these motions execute, a promotion straight onto the council.
func TestEvaluateProposal_EmptyElectorateNeverPasses(t *testing.T) {
	if got := evaluateProposal(0, 0, 0); got != domain.ProposalOpen {
		t.Errorf("empty electorate with no votes = %q, want open", got)
	}
}

// The person being removed does not vote on their own removal, so they are not
// in the denominator either. Every other motion is decided by the whole
// council.
func TestElectorateFor(t *testing.T) {
	tests := []struct {
		name            string
		t               domain.ProposalType
		councilSize     int64
		targetInCouncil bool
		want            int64
	}{
		{"promotion counts the whole council", domain.ProposalCouncilPromotion, 5, false, 5},
		{"bootstrap re-entry counts the whole council", domain.ProposalBootstrapReentry, 5, false, 5},
		{"removal excludes the target", domain.ProposalCouncilRemoval, 5, true, 4},
		{"removal of somebody already gone excludes nobody", domain.ProposalCouncilRemoval, 5, false, 5},
		{"removal from a council of two leaves one voter", domain.ProposalCouncilRemoval, 2, true, 1},
		// A promotion's target is a moderator while the vote runs, so this
		// case only arises once the motion has carried and put them on the
		// council. Their new seat must not be counted against the motion that
		// created it — see electorateFor.
		{"promotion excludes a target already promoted", domain.ProposalCouncilPromotion, 6, true, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := electorateFor(tt.t, tt.councilSize, tt.targetInCouncil); got != tt.want {
				t.Errorf("electorateFor(%q, %d, %v) = %d, want %d",
					tt.t, tt.councilSize, tt.targetInCouncil, got, tt.want)
			}
		})
	}
}

// The removal rule is a statement about who may vote, not an off-by-one to be
// tuned. On a council of four, counting the target would require three of the
// remaining three — unanimity among the target's colleagues — where the rule
// says three of four. Excluding them requires two of three.
func TestElectorateFor_RemovalDoesNotDemandUnanimityOfTheRest(t *testing.T) {
	const council = int64(4)

	electorate := electorateFor(domain.ProposalCouncilRemoval, council, true)
	needed := simpleMajority(electorate)

	if electorate != council-1 {
		t.Fatalf("electorate = %d, want the council minus the target (%d)", electorate, council-1)
	}
	if needed >= electorate {
		t.Errorf("a removal on a council of %d needs %d of %d — that is unanimity among the target's colleagues",
			council, needed, electorate)
	}
}

// The recount a repair takes after a promotion has executed must agree with the
// count that carried the motion. A council of three that passes a promotion 2-0
// becomes a council of four; if the new seat counted, the recount would need
// three and the motion would sit open forever with its target already promoted.
func TestElectorateFor_PromotionRecountAgreesWithTheCountThatCarriedIt(t *testing.T) {
	const councilBefore = int64(3)
	const approvals = int64(2)

	atVote := electorateFor(domain.ProposalCouncilPromotion, councilBefore, false)
	if approvals < simpleMajority(atVote) {
		t.Fatalf("%d approvals do not carry a motion before an electorate of %d", approvals, atVote)
	}

	// Executed: the target now holds a seat, so the council is one larger.
	afterExecution := electorateFor(domain.ProposalCouncilPromotion, councilBefore+1, true)
	if approvals < simpleMajority(afterExecution) {
		t.Errorf("after execution the same %d approvals no longer carry the motion "+
			"(electorate %d, majority %d) — the recount disagrees with the count",
			approvals, afterExecution, simpleMajority(afterExecution))
	}
}

// Every type must be one the executor knows, and every type that names a person
// must say so. A fourth type added to the domain without a branch in execute
// would otherwise be a motion the council can pass that does nothing.
func TestProposalType_ValidAndRequiresTarget(t *testing.T) {
	tests := []struct {
		t              domain.ProposalType
		valid          bool
		requiresTarget bool
	}{
		{domain.ProposalCouncilPromotion, true, true},
		{domain.ProposalCouncilRemoval, true, true},
		{domain.ProposalBootstrapReentry, true, false},
		{domain.ProposalType(""), false, false},
		{domain.ProposalType("council_expulsion"), false, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.t), func(t *testing.T) {
			if got := tt.t.Valid(); got != tt.valid {
				t.Errorf("Valid() = %v, want %v", got, tt.valid)
			}
			if got := tt.t.RequiresTarget(); got != tt.requiresTarget {
				t.Errorf("RequiresTarget() = %v, want %v", got, tt.requiresTarget)
			}
		})
	}
}

func TestTally(t *testing.T) {
	votes := []domain.CouncilVote{
		{VoterID: "a", Vote: domain.VoteApprove},
		{VoterID: "b", Vote: domain.VoteReject},
		{VoterID: "c", Vote: domain.VoteApprove},
	}

	approve, reject, mine := tally(votes, "b")
	if approve != 2 || reject != 1 {
		t.Errorf("tally = %d approve / %d reject, want 2/1", approve, reject)
	}
	if mine == nil || *mine != domain.VoteReject {
		t.Errorf("my vote = %v, want reject", mine)
	}

	// A council member who has not voted gets nil, not a zero value that would
	// render as "approve" on the client.
	if _, _, none := tally(votes, "d"); none != nil {
		t.Errorf("my vote for a non-voter = %v, want nil", *none)
	}
}

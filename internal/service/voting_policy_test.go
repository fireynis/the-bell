package service

import (
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
)

func TestSimpleMajority(t *testing.T) {
	tests := []struct {
		council int64
		want    int64
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
		if got := simpleMajority(tt.council); got != tt.want {
			t.Errorf("simpleMajority(%d) = %d, want %d", tt.council, got, tt.want)
		}
	}
}

// A majority must be strictly more than half, so an even council cannot decide
// on a tie — 2 of 4 is not enough either way.
func TestSimpleMajority_EvenCouncilNeedsMoreThanHalf(t *testing.T) {
	for _, council := range []int64{2, 4, 6, 10} {
		half := council / 2
		if got := simpleMajority(council); got <= half {
			t.Errorf("simpleMajority(%d) = %d, want more than half (%d)", council, got, half)
		}
	}
}

func TestEvaluateProposal(t *testing.T) {
	tests := []struct {
		name    string
		approve int64
		reject  int64
		council int64
		want    domain.ProposalStatus
	}{
		{"no votes yet", 0, 0, 5, domain.ProposalPending},
		{"approval majority of five", 3, 0, 5, domain.ProposalApproved},
		{"one short of approval", 2, 0, 5, domain.ProposalPending},
		{"rejection majority of five", 0, 3, 5, domain.ProposalRejected},
		{"one short of rejection", 0, 2, 5, domain.ProposalPending},
		{"split vote stays pending", 2, 2, 5, domain.ProposalPending},
		{"even council tie stays pending", 2, 2, 4, domain.ProposalPending},
		{"even council decided", 3, 1, 4, domain.ProposalApproved},
		{"unanimous approval", 5, 0, 5, domain.ProposalApproved},
		{"unanimous rejection", 0, 5, 5, domain.ProposalRejected},
		{"single member council approves alone", 1, 0, 1, domain.ProposalApproved},
		{"empty council can never decide", 5, 5, 0, domain.ProposalPending},
		{"negative council is treated as empty", 3, 0, -1, domain.ProposalPending},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateProposal(tt.approve, tt.reject, tt.council)
			if got != tt.want {
				t.Errorf("evaluateProposal(%d, %d, %d) = %q, want %q",
					tt.approve, tt.reject, tt.council, got, tt.want)
			}
		})
	}
}

// An empty council must not auto-approve: a miscounted or not-yet-seeded
// council would otherwise pass every proposal with zero votes.
func TestEvaluateProposal_EmptyCouncilNeverApproves(t *testing.T) {
	if got := evaluateProposal(0, 0, 0); got != domain.ProposalPending {
		t.Errorf("empty council with no votes = %q, want pending", got)
	}
}

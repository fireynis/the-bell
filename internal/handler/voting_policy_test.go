package handler

import (
	"errors"
	"testing"
)

func TestValidateCastVote(t *testing.T) {
	tests := []struct {
		name string
		req  castVoteRequest
		want error
	}{
		{"approve", castVoteRequest{ProposalID: "promote:user-1", Vote: "approve"}, nil},
		{"reject", castVoteRequest{ProposalID: "promote:user-1", Vote: "reject"}, nil},
		{"missing proposal id", castVoteRequest{Vote: "approve"}, errMissingProposalID},
		{"empty request", castVoteRequest{}, errMissingProposalID},
		{"missing vote", castVoteRequest{ProposalID: "promote:user-1"}, errInvalidVoteChoice},
		{"unknown choice", castVoteRequest{ProposalID: "promote:user-1", Vote: "abstain"}, errInvalidVoteChoice},
		{"wrong case", castVoteRequest{ProposalID: "promote:user-1", Vote: "Approve"}, errInvalidVoteChoice},
		{"padded choice", castVoteRequest{ProposalID: "promote:user-1", Vote: "approve "}, errInvalidVoteChoice},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCastVote(tt.req)
			if !errors.Is(err, tt.want) {
				t.Errorf("validateCastVote() = %v, want %v", err, tt.want)
			}
		})
	}
}

// A missing proposal id is reported before the choice is looked at, so the
// caller is told about the field they omitted rather than one they did not.
func TestValidateCastVote_ChecksProposalIDFirst(t *testing.T) {
	err := validateCastVote(castVoteRequest{ProposalID: "", Vote: "abstain"})

	if !errors.Is(err, errMissingProposalID) {
		t.Errorf("err = %v, want %v", err, errMissingProposalID)
	}
}

// These messages are written straight into the 400 response body, so they must
// describe what the caller got wrong.
func TestValidateCastVote_MessagesAreClientFacing(t *testing.T) {
	if got := errMissingProposalID.Error(); got != "proposal_id is required" {
		t.Errorf("message = %q, want %q", got, "proposal_id is required")
	}
	if got := errInvalidVoteChoice.Error(); got != "vote must be 'approve' or 'reject'" {
		t.Errorf("message = %q, want %q", got, "vote must be 'approve' or 'reject'")
	}
}

package domain

import "time"

// VoteChoice is one council member's answer on one motion. There is no
// abstention: not voting is how a council member abstains, and recording it
// would make the tally count people who declined to decide.
type VoteChoice string

const (
	VoteApprove VoteChoice = "approve"
	VoteReject  VoteChoice = "reject"
)

// Valid reports whether c is one of the two recordable choices. A vote outside
// this set would be stored and then counted by neither tally.
func (c VoteChoice) Valid() bool {
	return c == VoteApprove || c == VoteReject
}

// CouncilVote is one vote on one proposal. The (proposal_id, voter_id) pair is
// unique in the schema, which is what enforces one vote per council member.
//
// It carries no JSON tags any more. Votes are never serialized: the API
// publishes tallies and the caller's own choice, not a roll call of who voted
// which way, so nothing may accidentally put a named ballot on the wire.
type CouncilVote struct {
	ID         string
	ProposalID string
	VoterID    string
	Vote       VoteChoice
	CreatedAt  time.Time
}

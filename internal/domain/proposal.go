package domain

import "time"

// ProposalType is what a council motion asks the town to do.
//
// The list is closed, and every member of it names something the service
// executes when the vote carries. That is the point: council_votes has existed
// since the town's first migration recording opinions about proposal ids that
// referred to nothing, and a type nothing acts on would rebuild exactly that.
type ProposalType string

const (
	// ProposalCouncilPromotion raises an active moderator to the council.
	ProposalCouncilPromotion ProposalType = "council_promotion"
	// ProposalCouncilRemoval returns a council member to ordinary membership.
	ProposalCouncilRemoval ProposalType = "council_removal"
	// ProposalBootstrapReentry puts the town back into bootstrap mode, where
	// the council admits residents directly instead of waiting for vouches.
	ProposalBootstrapReentry ProposalType = "bootstrap_reentry"
)

// RequiresTarget reports whether this type of motion is about a person.
//
// bootstrap_reentry is about the town, so it has no target and the service
// refuses one — a motion carrying a target nobody will act on would look like a
// motion about that person.
func (t ProposalType) RequiresTarget() bool {
	return t == ProposalCouncilPromotion || t == ProposalCouncilRemoval
}

// Valid reports whether t is one of the three known types. Anything else is a
// caller's typo, not a motion.
func (t ProposalType) Valid() bool {
	switch t {
	case ProposalCouncilPromotion, ProposalCouncilRemoval, ProposalBootstrapReentry:
		return true
	default:
		return false
	}
}

// ProposalStatus is where a motion stands.
//
// These are the words a town uses about a motion rather than the
// pending/approved/rejected the previous shell spoke in, and they match the
// CHECK constraint in migration 00021 exactly, so a status the database would
// refuse cannot be constructed here.
type ProposalStatus string

const (
	// ProposalOpen means the council is still voting.
	ProposalOpen ProposalStatus = "open"
	// ProposalPassed means a majority approved and the motion was executed.
	ProposalPassed ProposalStatus = "passed"
	// ProposalRejected means a majority refused, or the motion could no longer
	// be executed when its moment came — see the execution notes on
	// service.ProposalService.
	ProposalRejected ProposalStatus = "rejected"
)

// Proposal is one motion before the council.
//
// TargetUserID is the empty string rather than a nil pointer when the motion
// has no target. Every reader of it asks "is there a target", and one spelling
// of absence is enough; the column stays nullable because a foreign key cannot
// point at "".
type Proposal struct {
	ID           string
	Type         ProposalType
	TargetUserID string
	Rationale    string
	CreatedBy    string
	Status       ProposalStatus
	CreatedAt    time.Time
	DecidedAt    *time.Time
}

// ProposalView is a motion as one particular council member sees it: the motion
// itself, its running tally, and how that member voted.
//
// CouncilSize is the electorate for THIS motion, not the size of the council.
// The two differ for a removal, where the person being removed does not vote,
// and a client rendering "3 of 5" from a council count would show a threshold
// nobody could reach.
//
// MyVote is nil when the viewer has not voted. It is per-caller, so a view is
// never cached or shared between council members.
type ProposalView struct {
	Proposal
	TargetDisplayName    string
	CreatedByDisplayName string
	ApproveCount         int64
	RejectCount          int64
	CouncilSize          int64
	MyVote               *VoteChoice
}

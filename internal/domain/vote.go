package domain

import "time"

type VoteChoice string

const (
	VoteApprove VoteChoice = "approve"
	VoteReject  VoteChoice = "reject"
)

type CouncilVote struct {
	ID         string     `json:"id"`
	ProposalID string     `json:"proposal_id"`
	VoterID    string     `json:"voter_id"`
	Vote       VoteChoice `json:"vote"`
	CreatedAt  time.Time  `json:"created_at"`
}

type ProposalStatus string

const (
	ProposalPending  ProposalStatus = "pending"
	ProposalApproved ProposalStatus = "approved"
	ProposalRejected ProposalStatus = "rejected"
)

type ProposalSummary struct {
	ProposalID   string         `json:"proposal_id"`
	ApproveCount int64          `json:"approve_count"`
	RejectCount  int64          `json:"reject_count"`
	TotalCouncil int64          `json:"total_council"`
	Status       ProposalStatus `json:"status"`
	Votes        []CouncilVote  `json:"votes"`
}

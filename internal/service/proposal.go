package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/google/uuid"
)

// maxRationaleLength bounds the text of a motion, in runes.
//
// A rationale is the case the proposer makes to the rest of the council, and it
// is the only record of why a role changed hands — role_history's reason is
// generated from it. A thousand characters is several paragraphs, which is more
// than any of the three motions needs and far less than a place to paste a
// transcript.
const maxRationaleLength = 1000

// minCouncilSize is how small the council may be left. Removing the last
// council member would leave the town with nobody who can approve a resident,
// change configuration, or vote on putting a council back — including nobody
// who could vote on undoing the removal.
const minCouncilSize = 1

// simpleMajority returns the number of votes needed to carry a motion before an
// electorate of the given size: strictly more than half.
func simpleMajority(electorate int64) int64 {
	if electorate <= 0 {
		return 0
	}
	return electorate/2 + 1
}

// evaluateProposal decides a motion's status from its tallies. It is decided as
// soon as either side reaches a simple majority of the electorate; approval is
// checked first so an electorate that somehow recorded a majority both ways
// resolves to passed.
//
// An electorate of zero has no reachable majority and always stays open, which
// keeps an empty or miscounted council from carrying everything unopposed.
func evaluateProposal(approveCount, rejectCount, electorate int64) domain.ProposalStatus {
	majority := simpleMajority(electorate)
	if majority <= 0 {
		return domain.ProposalOpen
	}
	switch {
	case approveCount >= majority:
		return domain.ProposalPassed
	case rejectCount >= majority:
		return domain.ProposalRejected
	default:
		return domain.ProposalOpen
	}
}

// electorateFor returns how many council members may vote on a motion.
//
// The rule is one sentence: a motion about a person is never decided with that
// person's own seat in the denominator.
//
// For a removal that is the whole point. They do not get a say in whether they
// keep their seat, so counting them would raise the bar their colleagues have
// to clear — on a council of four, 3-of-4 rather than the 2-of-3 the rule
// intends.
//
// For a promotion it changes nothing while the motion is being voted on, since
// the target is a moderator and holds no seat. It matters afterwards. Executing
// a promotion puts the target ON the council, so a recount taken after
// execution — which is what the repair in List does — would find the electorate
// one larger than it was when the motion carried, and could conclude the motion
// no longer has its majority. A council of three that passed a promotion 2-0
// would become a council of four needing three, and the motion would sit open
// forever with its target already promoted. Excluding the target's own seat is
// what keeps the recount agreeing with the count.
//
// targetInCouncil is false once the target has left the council by any route,
// and then there is nobody to subtract: they are already outside the count.
func electorateFor(t domain.ProposalType, councilSize int64, targetInCouncil bool) int64 {
	if t.RequiresTarget() && targetInCouncil {
		return councilSize - 1
	}
	return councilSize
}

// ProposalStore is the proposal persistence a ProposalService needs.
type ProposalStore interface {
	CreateProposal(ctx context.Context, p *domain.Proposal) error
	GetProposal(ctx context.Context, id string) (*domain.Proposal, error)
	ListOpenProposals(ctx context.Context) ([]domain.ProposalView, error)
	ListDecidedProposals(ctx context.Context, limit int) ([]domain.ProposalView, error)
	// FindOpenProposalByTypeAndTarget returns ErrNotFound when there is none.
	// targetID is the empty string for a motion with no target.
	FindOpenProposalByTypeAndTarget(ctx context.Context, t domain.ProposalType, targetID string) (*domain.Proposal, error)
	// DecideProposal flips a motion out of 'open'. It returns ErrNotFound when
	// the motion is already decided, which is how a caller learns another
	// request settled it first.
	DecideProposal(ctx context.Context, id string, status domain.ProposalStatus, decidedAt time.Time) error
}

// VoteRepository is the vote persistence a ProposalService needs.
//
// There is no "has this member voted" method: every caller of it already reads
// the whole ballot to build the tally, and a proposal's votes are bounded by
// the size of the council.
type VoteRepository interface {
	CreateVote(ctx context.Context, vote *domain.CouncilVote) error
	ListVotesByProposal(ctx context.Context, proposalID string) ([]domain.CouncilVote, error)
	CountCouncilMembers(ctx context.Context) (int64, error)
}

// ProposalUserRepository is the user persistence a ProposalService needs: it
// reads the people motions are about and writes the role changes that carrying
// one produces.
type ProposalUserRepository interface {
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
	UpdateUserRole(ctx context.Context, id string, role domain.Role) error
	CountActiveMembers(ctx context.Context) (int64, error)
}

// RoleHistoryWriter records a role change in the audit trail.
//
// Declared here rather than reusing RoleCheckerRepository so this service
// depends on the one write it makes, in the same spirit as TrustScoreWriter.
// Every path that changes a role writes one of these, and a council vote is no
// exception — a seat changing hands by a vote of the town's council is the
// least explicable role change to leave unrecorded.
type RoleHistoryWriter interface {
	CreateRoleHistoryEntry(ctx context.Context, entry *domain.RoleHistory) error
}

// ProposalService runs council motions: raising them, voting on them, and
// carrying out what a passing vote decided.
//
// Executing is the part that did not exist before. The old voting service
// recorded votes against proposal ids that referred to nothing and computed a
// status nobody read, so a council could vote unanimously to promote someone
// and nothing whatsoever would happen. Every type here changes something.
type ProposalService struct {
	proposals   ProposalStore
	votes       VoteRepository
	users       ProposalUserRepository
	roleHistory RoleHistoryWriter
	config      ConfigRepository
	logger      *slog.Logger
	now         func() time.Time
}

func NewProposalService(
	proposals ProposalStore,
	votes VoteRepository,
	users ProposalUserRepository,
	roleHistory RoleHistoryWriter,
	config ConfigRepository,
	logger *slog.Logger,
	clock func() time.Time,
) *ProposalService {
	if clock == nil {
		clock = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &ProposalService{
		proposals:   proposals,
		votes:       votes,
		users:       users,
		roleHistory: roleHistory,
		config:      config,
		logger:      logger,
		now:         clock,
	}
}

// Every entry point here goes through requireCouncil, which this package
// already declares for the one moderation action that exceeds a moderator's
// authority. The route group carries a council guard too; the service checks
// again for the reason every other service does — the policy belongs with the
// operation, not with whichever router happens to reach it.

// Create raises a motion.
//
// Each type has its own precondition, and each one is checked here rather than
// left to the vote:
//
//   - council_promotion needs a target who is an active moderator. Promoting
//     straight from member skips the standing the moderator role represents,
//     and the role checker's own promotion path is where that is earned.
//   - council_removal needs a target who is on the council, and needs the
//     council to be big enough that removing one leaves somebody behind.
//   - bootstrap_reentry needs no target, needs the town to be OUT of bootstrap
//     mode, and needs the active member count to be below the exit threshold.
//     That last one is not decoration: exitBootstrapIfEarned turns bootstrap
//     mode off again the moment an approval-path call sees the town at or above
//     the threshold, so a motion raised in a big town would pass, set the flag,
//     and have it cleared by the next council approval — the council would have
//     voted for something the system undoes on its own. Better to refuse it
//     with that written on the refusal.
func (s *ProposalService) Create(ctx context.Context, creator *domain.User, t domain.ProposalType, targetID, rationale string) (*domain.ProposalView, error) {
	if err := requireCouncil(creator); err != nil {
		return nil, err
	}
	if !t.Valid() {
		return nil, fmt.Errorf("%w: unknown proposal type %q", ErrValidation, t)
	}

	rationale = strings.TrimSpace(rationale)
	if rationale == "" {
		return nil, fmt.Errorf("%w: rationale is required", ErrValidation)
	}
	if utf8.RuneCountInString(rationale) > maxRationaleLength {
		return nil, fmt.Errorf("%w: rationale exceeds %d characters", ErrValidation, maxRationaleLength)
	}

	targetID = strings.TrimSpace(targetID)
	target, err := s.validateTarget(ctx, t, targetID)
	if err != nil {
		return nil, err
	}

	if err := s.rejectDuplicate(ctx, t, targetID); err != nil {
		return nil, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generating proposal id: %w", err)
	}

	p := &domain.Proposal{
		ID:           id.String(),
		Type:         t,
		TargetUserID: targetID,
		Rationale:    rationale,
		CreatedBy:    creator.ID,
		Status:       domain.ProposalOpen,
		CreatedAt:    s.now(),
	}
	if err := s.proposals.CreateProposal(ctx, p); err != nil {
		return nil, fmt.Errorf("creating proposal: %w", err)
	}

	view := &domain.ProposalView{
		Proposal:             *p,
		CreatedByDisplayName: creator.DisplayName,
	}
	if target != nil {
		view.TargetDisplayName = target.DisplayName
	}
	// A motion opens with no votes, so the electorate is the only part of the
	// tally worth computing — and it is the part the proposer needs, because it
	// is what tells them how many colleagues have to agree.
	view.CouncilSize, err = s.electorate(ctx, p)
	if err != nil {
		return nil, err
	}
	return view, nil
}

// validateTarget checks the person a motion is about and returns them, or nil
// for a motion that is about the town.
func (s *ProposalService) validateTarget(ctx context.Context, t domain.ProposalType, targetID string) (*domain.User, error) {
	if !t.RequiresTarget() {
		if targetID != "" {
			return nil, fmt.Errorf("%w: a %s proposal is about the town and takes no target", ErrValidation, t)
		}
		return nil, s.validateBootstrapReentry(ctx)
	}

	if targetID == "" {
		return nil, fmt.Errorf("%w: a %s proposal requires a target user", ErrValidation, t)
	}
	target, err := s.users.GetUserByID(ctx, targetID)
	if err != nil {
		return nil, fmt.Errorf("looking up target: %w", err)
	}

	switch t {
	case domain.ProposalCouncilPromotion:
		if target.Role != domain.RoleModerator || !target.IsActive {
			return nil, fmt.Errorf("%w: only an active moderator can be proposed for the council", ErrValidation)
		}
	case domain.ProposalCouncilRemoval:
		if target.Role != domain.RoleCouncil {
			return nil, fmt.Errorf("%w: only a council member can be proposed for removal", ErrValidation)
		}
		council, err := s.votes.CountCouncilMembers(ctx)
		if err != nil {
			return nil, fmt.Errorf("counting council members: %w", err)
		}
		if council <= minCouncilSize {
			return nil, fmt.Errorf("%w: the council cannot be left empty; there must be at least %d member",
				ErrValidation, minCouncilSize)
		}
	}
	return target, nil
}

// validateBootstrapReentry checks the two conditions that make re-entry
// meaningful: the town is not already in bootstrap mode, and it is small enough
// that the mode would survive being switched on.
func (s *ProposalService) validateBootstrapReentry(ctx context.Context) error {
	mode, err := s.config.GetTownConfig(ctx, "bootstrap_mode")
	if err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("reading bootstrap mode: %w", err)
	}
	if mode == "true" {
		return fmt.Errorf("%w: the town is already in bootstrap mode", ErrValidation)
	}

	members, err := s.users.CountActiveMembers(ctx)
	if err != nil {
		return fmt.Errorf("counting active members: %w", err)
	}
	if members >= bootstrapExitThreshold {
		return fmt.Errorf("%w: the town has %d active members, at or above the %d that ends bootstrap mode, "+
			"so re-entering it would be undone by the next approval",
			ErrValidation, members, bootstrapExitThreshold)
	}
	return nil
}

// rejectDuplicate refuses a second open motion on the same question.
//
// The partial unique index in 00021 is what guarantees this; the lookup is here
// so the council reads "there is already an open proposal" rather than a
// constraint violation. Two open motions on one question would split the
// council's votes between them and neither would reach a majority.
func (s *ProposalService) rejectDuplicate(ctx context.Context, t domain.ProposalType, targetID string) error {
	existing, err := s.proposals.FindOpenProposalByTypeAndTarget(ctx, t, targetID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("checking for an open proposal: %w", err)
	}
	if existing != nil {
		return fmt.Errorf("%w: there is already an open %s proposal", ErrValidation, t)
	}
	return nil
}

// Vote records one council member's choice and returns the motion as they now
// see it, including any decision and execution the vote just triggered.
//
// The caller gets the settled view rather than an acknowledgement, so the
// council member who casts the deciding vote sees the outcome in the same
// response instead of having to reload and wonder whether anything happened.
func (s *ProposalService) Vote(ctx context.Context, voter *domain.User, proposalID string, approve bool) (*domain.ProposalView, error) {
	if err := requireCouncil(voter); err != nil {
		return nil, err
	}
	if proposalID == "" {
		return nil, fmt.Errorf("%w: proposal id is required", ErrValidation)
	}

	p, err := s.proposals.GetProposal(ctx, proposalID)
	if err != nil {
		return nil, fmt.Errorf("looking up proposal: %w", err)
	}
	if p.Status != domain.ProposalOpen {
		return nil, fmt.Errorf("%w: this proposal has already been decided", ErrValidation)
	}

	// Nobody votes on their own removal. The electorate excludes them for the
	// same reason — see electorateFor — so a vote recorded here would be
	// counted against a denominator it was not part of.
	if p.Type == domain.ProposalCouncilRemoval && p.TargetUserID == voter.ID {
		return nil, fmt.Errorf("%w: you cannot vote on your own removal", ErrForbidden)
	}

	votes, err := s.votes.ListVotesByProposal(ctx, proposalID)
	if err != nil {
		return nil, fmt.Errorf("listing votes: %w", err)
	}
	for _, v := range votes {
		if v.VoterID == voter.ID {
			return nil, fmt.Errorf("%w: you have already voted on this proposal", ErrValidation)
		}
	}

	choice := domain.VoteReject
	if approve {
		choice = domain.VoteApprove
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generating vote id: %w", err)
	}
	vote := domain.CouncilVote{
		ID:         id.String(),
		ProposalID: proposalID,
		VoterID:    voter.ID,
		Vote:       choice,
		CreatedAt:  s.now(),
	}
	// The unique index on (proposal_id, voter_id) is the real guard against
	// double voting; the scan above is what turns it into a sentence. Two
	// requests racing still land here, and the adapter maps the violation to
	// ErrValidation.
	if err := s.votes.CreateVote(ctx, &vote); err != nil {
		return nil, fmt.Errorf("casting vote: %w", err)
	}
	votes = append(votes, vote)

	return s.settle(ctx, p, votes, voter.ID)
}

// List returns the open motions or the decided ones, as the calling council
// member sees them.
//
// Listing the open ones also repairs any that should already have been decided.
// That is this service's version of the second chance requireBootstrap takes at
// the bootstrap exit, and it exists for the same reason: the decision and the
// execution happen after the deciding vote has already committed, so a failure
// there leaves a motion sitting open with a majority already recorded. No
// further vote can arrive to re-evaluate it once every council member has
// voted, and this is the call the council makes when it wonders why nothing
// happened.
//
// A repair failure is logged and not propagated, exactly as
// exitBootstrapIfEarned does: the caller asked for a list, and a council that
// cannot repair a stuck motion should still be able to read its queue.
func (s *ProposalService) List(ctx context.Context, viewer *domain.User, decided bool, limit int) ([]domain.ProposalView, error) {
	if err := requireCouncil(viewer); err != nil {
		return nil, err
	}

	if decided {
		views, err := s.proposals.ListDecidedProposals(ctx, limit)
		if err != nil {
			return nil, fmt.Errorf("listing decided proposals: %w", err)
		}
		return s.attachTallies(ctx, views, viewer.ID)
	}

	views, err := s.proposals.ListOpenProposals(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing open proposals: %w", err)
	}
	views, err = s.attachTallies(ctx, views, viewer.ID)
	if err != nil {
		return nil, err
	}

	// A motion repaired here is no longer open, so it leaves this listing and
	// turns up in the decided one. Returning it among the open motions would
	// hand the council a queue containing something already settled.
	stillOpen := views[:0]
	for i := range views {
		settled, err := s.settleIfReached(ctx, &views[i].Proposal, views[i].ApproveCount, views[i].RejectCount, views[i].CouncilSize)
		if err != nil {
			s.logger.Warn("re-deciding a proposal whose majority was already reached failed",
				"proposal_id", views[i].ID, "type", views[i].Type, "error", err)
			stillOpen = append(stillOpen, views[i])
			continue
		}
		if settled == nil {
			stillOpen = append(stillOpen, views[i])
			continue
		}
		s.logger.Info("proposal decided on a later listing; the deciding vote's own attempt must have failed",
			"proposal_id", settled.ID, "type", settled.Type, "status", settled.Status)
	}
	return stillOpen, nil
}

// attachTallies fills in the tally, electorate and the viewer's own vote for a
// page of motions.
//
// The council is counted once for the whole page rather than per motion, which
// is the same economy the old summary builder made; the ballot itself is one
// read per motion, over a handful of rows each.
func (s *ProposalService) attachTallies(ctx context.Context, views []domain.ProposalView, viewerID string) ([]domain.ProposalView, error) {
	if len(views) == 0 {
		return views, nil
	}

	council, err := s.votes.CountCouncilMembers(ctx)
	if err != nil {
		return nil, fmt.Errorf("counting council members: %w", err)
	}

	for i := range views {
		votes, err := s.votes.ListVotesByProposal(ctx, views[i].ID)
		if err != nil {
			return nil, fmt.Errorf("listing votes for %s: %w", views[i].ID, err)
		}
		views[i].ApproveCount, views[i].RejectCount, views[i].MyVote = tally(votes, viewerID)

		targetInCouncil, err := s.targetInCouncil(ctx, &views[i].Proposal)
		if err != nil {
			return nil, err
		}
		views[i].CouncilSize = electorateFor(views[i].Type, council, targetInCouncil)
	}
	return views, nil
}

// tally counts a ballot and picks out the viewer's own vote.
func tally(votes []domain.CouncilVote, viewerID string) (approve, reject int64, mine *domain.VoteChoice) {
	for _, v := range votes {
		switch v.Vote {
		case domain.VoteApprove:
			approve++
		case domain.VoteReject:
			reject++
		}
		if v.VoterID == viewerID {
			choice := v.Vote
			mine = &choice
		}
	}
	return approve, reject, mine
}

// settle computes the tally for one motion, decides it if a majority has been
// reached, and returns the view the caller should see.
func (s *ProposalService) settle(ctx context.Context, p *domain.Proposal, votes []domain.CouncilVote, viewerID string) (*domain.ProposalView, error) {
	approve, reject, mine := tally(votes, viewerID)

	electorate, err := s.electorate(ctx, p)
	if err != nil {
		return nil, err
	}

	settled, err := s.settleIfReached(ctx, p, approve, reject, electorate)
	if err != nil {
		return nil, err
	}
	if settled != nil {
		p = settled
	}

	view := &domain.ProposalView{
		Proposal:     *p,
		ApproveCount: approve,
		RejectCount:  reject,
		CouncilSize:  electorate,
		MyVote:       mine,
	}
	if err := s.attachNames(ctx, view); err != nil {
		return nil, err
	}
	return view, nil
}

// settleIfReached decides and executes a motion whose tally has reached a
// majority, returning the decided motion — or nil when it is still open.
//
// Execution happens BEFORE the status is written, and the order is deliberate.
// Writing 'passed' first and then failing to execute would leave a motion that
// says it carried and did nothing, with no state left that says otherwise. This
// way a failure leaves the motion open with its majority intact, which List
// finds and finishes. That is only safe because every execution is idempotent —
// see execute — so finishing a motion that already half-executed cannot promote
// somebody twice.
func (s *ProposalService) settleIfReached(ctx context.Context, p *domain.Proposal, approve, reject, electorate int64) (*domain.Proposal, error) {
	status := evaluateProposal(approve, reject, electorate)
	if status == domain.ProposalOpen {
		return nil, nil
	}

	if status == domain.ProposalPassed {
		executed, err := s.execute(ctx, p)
		if err != nil {
			return nil, err
		}
		if !executed {
			// The motion carried but can no longer be carried out — the target
			// is no longer who they were when it was raised, or the town has
			// grown past the point where re-entering bootstrap mode would
			// stick. Recording it as rejected is the honest outcome: nothing
			// happened, and a 'passed' motion that changed nothing would be a
			// lie in the council's own record.
			status = domain.ProposalRejected
		}
	}

	decidedAt := s.now()
	if err := s.proposals.DecideProposal(ctx, p.ID, status, decidedAt); err != nil {
		if errors.Is(err, ErrNotFound) {
			// Another request settled it between our read and our write. Theirs
			// stands; re-read rather than reporting ours.
			return s.proposals.GetProposal(ctx, p.ID)
		}
		return nil, fmt.Errorf("deciding proposal: %w", err)
	}

	decided := *p
	decided.Status = status
	decided.DecidedAt = &decidedAt
	return &decided, nil
}

// execute carries out a motion that has passed, reporting whether it could be.
//
// A false return is not a failure: it means the motion is no longer applicable
// — the moderator being promoted has since been demoted, the council member
// being removed has already left, the town has grown past the bootstrap
// threshold — and the caller records the motion as rejected. Only a genuine
// persistence failure comes back as an error, and that one leaves the motion
// open to be finished later.
//
// Every branch is idempotent, checking the end state before writing. That is
// what makes the retry in List safe: a motion whose role change committed but
// whose status write failed will, on the next pass, find the end state already
// true, report success without writing a second role_history row, and get its
// status.
func (s *ProposalService) execute(ctx context.Context, p *domain.Proposal) (bool, error) {
	switch p.Type {
	case domain.ProposalCouncilPromotion:
		return s.executeRoleChange(ctx, p, domain.RoleModerator, domain.RoleCouncil)
	case domain.ProposalCouncilRemoval:
		return s.executeRemoval(ctx, p)
	case domain.ProposalBootstrapReentry:
		return s.executeBootstrapReentry(ctx, p)
	default:
		// Unreachable while the CHECK constraint and ProposalType.Valid agree,
		// and an error rather than a silent success so that a fourth type added
		// to one without the other cannot pass a motion that does nothing.
		return false, fmt.Errorf("unhandled proposal type %q", p.Type)
	}
}

// executeRoleChange moves a target from one role to another, treating a target
// already at the destination as done.
func (s *ProposalService) executeRoleChange(ctx context.Context, p *domain.Proposal, from, to domain.Role) (bool, error) {
	target, err := s.users.GetUserByID(ctx, p.TargetUserID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// The account is gone. Nothing to promote and nothing to report but
			// the motion's inapplicability.
			return false, nil
		}
		return false, fmt.Errorf("looking up target: %w", err)
	}
	if target.Role == to {
		return true, nil
	}
	if target.Role != from || !target.IsActive {
		return false, nil
	}
	return true, s.changeRole(ctx, target, to, p)
}

// executeRemoval returns a council member to ordinary membership, refusing to
// empty the council.
//
// The council size is re-checked here and not only at creation, because the
// council can shrink while a motion is open — two simultaneous removals, each
// legal when raised, must not between them leave the town with no council.
func (s *ProposalService) executeRemoval(ctx context.Context, p *domain.Proposal) (bool, error) {
	target, err := s.users.GetUserByID(ctx, p.TargetUserID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("looking up target: %w", err)
	}
	if target.Role != domain.RoleCouncil {
		// Already off the council, by this motion's earlier half-completion or
		// by any other route. The end state holds either way.
		return target.Role == domain.RoleMember, nil
	}

	council, err := s.votes.CountCouncilMembers(ctx)
	if err != nil {
		return false, fmt.Errorf("counting council members: %w", err)
	}
	if council <= minCouncilSize {
		return false, nil
	}

	return true, s.changeRole(ctx, target, domain.RoleMember, p)
}

// executeBootstrapReentry switches bootstrap mode back on.
//
// The membership check runs again for the reason it ran at creation: bootstrap
// mode set in a town at or above the exit threshold is cleared again by the
// next approval-path call, so turning it on would produce a motion that
// "passed" and left nothing behind.
func (s *ProposalService) executeBootstrapReentry(ctx context.Context, _ *domain.Proposal) (bool, error) {
	mode, err := s.config.GetTownConfig(ctx, "bootstrap_mode")
	if err != nil && !errors.Is(err, ErrNotFound) {
		return false, fmt.Errorf("reading bootstrap mode: %w", err)
	}
	if mode == "true" {
		return true, nil
	}

	members, err := s.users.CountActiveMembers(ctx)
	if err != nil {
		return false, fmt.Errorf("counting active members: %w", err)
	}
	if members >= bootstrapExitThreshold {
		return false, nil
	}

	if err := s.config.SetTownConfig(ctx, "bootstrap_mode", "true"); err != nil {
		return false, fmt.Errorf("enabling bootstrap mode: %w", err)
	}
	return true, nil
}

// changeRole applies a role change and records it, the way every other role
// change in the codebase is recorded.
//
// The reason names the motion rather than quoting it: role_history.reason is
// read alongside a list of automatic promotions and demotions, and a paragraph
// of council debate pasted into that column would swamp them. The proposal id
// is the pointer to the rationale in full.
func (s *ProposalService) changeRole(ctx context.Context, target *domain.User, newRole domain.Role, p *domain.Proposal) error {
	// Read before the write. The audit entry's old_role is a fact about the
	// moment before the update, and reading it off target afterwards would
	// depend on the repository not having touched the value in hand.
	oldRole := target.Role

	if err := s.users.UpdateUserRole(ctx, target.ID, newRole); err != nil {
		return fmt.Errorf("updating role: %w", err)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generating role history id: %w", err)
	}
	entry := &domain.RoleHistory{
		ID:        id.String(),
		UserID:    target.ID,
		OldRole:   oldRole,
		NewRole:   newRole,
		Reason:    fmt.Sprintf("council vote: %s proposal %s passed", p.Type, p.ID),
		CreatedAt: s.now(),
	}
	if err := s.roleHistory.CreateRoleHistoryEntry(ctx, entry); err != nil {
		return fmt.Errorf("recording role history: %w", err)
	}
	return nil
}

// electorate returns how many council members may vote on this motion.
func (s *ProposalService) electorate(ctx context.Context, p *domain.Proposal) (int64, error) {
	council, err := s.votes.CountCouncilMembers(ctx)
	if err != nil {
		return 0, fmt.Errorf("counting council members: %w", err)
	}
	inCouncil, err := s.targetInCouncil(ctx, p)
	if err != nil {
		return 0, err
	}
	return electorateFor(p.Type, council, inCouncil), nil
}

// targetInCouncil reports whether the motion's target is currently counted
// among the council. Only a motion about a person asks, so bootstrap re-entry
// never pays for the lookup.
func (s *ProposalService) targetInCouncil(ctx context.Context, p *domain.Proposal) (bool, error) {
	if !p.Type.RequiresTarget() || p.TargetUserID == "" {
		return false, nil
	}
	target, err := s.users.GetUserByID(ctx, p.TargetUserID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("looking up target: %w", err)
	}
	return target.IsCouncil(), nil
}

// attachNames fills in the display names a single-motion response carries. The
// listings get theirs from the query's joins; this is for Create and Vote,
// which build one view from one motion.
//
// A name that cannot be read is left empty rather than failing the call: the
// client falls back to the id, which is the same thing it does for a resident
// who has set no name at all.
func (s *ProposalService) attachNames(ctx context.Context, view *domain.ProposalView) error {
	if view.CreatedBy != "" {
		if creator, err := s.users.GetUserByID(ctx, view.CreatedBy); err == nil {
			view.CreatedByDisplayName = creator.DisplayName
		} else if !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("looking up proposer: %w", err)
		}
	}
	if view.TargetUserID != "" {
		if target, err := s.users.GetUserByID(ctx, view.TargetUserID); err == nil {
			view.TargetDisplayName = target.DisplayName
		} else if !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("looking up target: %w", err)
		}
	}
	return nil
}

package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/google/uuid"
)

const maxActionReasonLen = 1000

// allowedSeverity maps each action type to its valid severity values.
var allowedSeverity = map[domain.ActionType][]int{
	domain.ActionWarn:    {1, 2},
	domain.ActionMute:    {3},
	domain.ActionSuspend: {4},
	domain.ActionBan:     {5},
}

// actionRequest is a validated moderation action request. Building one can
// only succeed if every rule in validateActionRequest passed, so the caller
// does not need to re-check the fields.
type actionRequest struct {
	ModeratorID  string
	TargetUserID string
	ActionType   domain.ActionType
	Severity     int
	Reason       string
	Duration     *time.Duration
}

// validateActionRequest enforces the rules governing what a moderator may do,
// independent of whether the target exists or the write succeeds:
//
//   - the action type must be known, and its severity must match (a "warn"
//     cannot carry ban-level severity, which is what drives the trust penalty)
//   - a reason is mandatory and bounded, because actions are shown in the
//     public audit trail
//   - nobody may moderate themselves
//   - mutes and suspensions are temporary and need a duration; bans are
//     permanent and must not have one
func validateActionRequest(
	moderatorID, targetUserID string,
	actionType domain.ActionType,
	severity int,
	reason string,
	durationSeconds *int64,
) (actionRequest, error) {
	allowed, ok := allowedSeverity[actionType]
	if !ok {
		return actionRequest{}, fmt.Errorf("%w: invalid action type %q", ErrValidation, actionType)
	}
	if !slices.Contains(allowed, severity) {
		return actionRequest{}, fmt.Errorf("%w: severity %d not valid for action type %q", ErrValidation, severity, actionType)
	}

	reason = strings.TrimSpace(reason)
	if reason == "" {
		return actionRequest{}, fmt.Errorf("%w: reason must not be empty", ErrValidation)
	}
	if len(reason) > maxActionReasonLen {
		return actionRequest{}, fmt.Errorf("%w: reason exceeds %d characters", ErrValidation, maxActionReasonLen)
	}

	if moderatorID == targetUserID {
		return actionRequest{}, fmt.Errorf("%w: cannot moderate yourself", ErrValidation)
	}

	if actionType == domain.ActionBan && durationSeconds != nil {
		return actionRequest{}, fmt.Errorf("%w: bans cannot have a duration", ErrValidation)
	}
	// An indefinite mute is a suspension, and we have one of those. Naming the
	// right tool beats refusing and leaving the moderator to guess.
	if actionType == domain.ActionMute && durationSeconds == nil {
		return actionRequest{}, fmt.Errorf(
			"%w: mute requires a duration; use suspend for an indefinite restriction", ErrValidation)
	}
	if actionType == domain.ActionSuspend && durationSeconds == nil {
		return actionRequest{}, fmt.Errorf("%w: suspend requires a duration", ErrValidation)
	}

	var duration *time.Duration
	if durationSeconds != nil {
		if *durationSeconds <= 0 {
			return actionRequest{}, fmt.Errorf("%w: duration must be positive", ErrValidation)
		}
		d := time.Duration(*durationSeconds) * time.Second
		duration = &d
	}

	return actionRequest{
		ModeratorID:  moderatorID,
		TargetUserID: targetUserID,
		ActionType:   actionType,
		Severity:     severity,
		Reason:       reason,
		Duration:     duration,
	}, nil
}

// enforcementStep is one immediate state change to apply to the target user.
type enforcementStep int

const (
	// enforceMute records when the mute expires on the user row.
	enforceMute enforcementStep = iota
	// enforceDeactivate suspends the account.
	enforceDeactivate
	// enforceBanRole moves the user to the banned role.
	enforceBanRole
	// enforceZeroTrust wipes the trust score.
	enforceZeroTrust
)

// planEnforcement decides which immediate state changes an action implies.
//
// A mute writes muted_until and nothing else. It used to push the user's trust
// just under the posting threshold instead, which the trust worker undid within
// seconds of the penalty landing — moderation state cannot live in a score a
// background job is free to recompute. The trust penalty the action propagates
// is punishment enough; the mute is a separate, authoritative fact.
//
// The user's current score is no longer an input: a mute applies even to
// someone already below the threshold, because their score may recover before
// the mute expires.
func planEnforcement(actionType domain.ActionType) []enforcementStep {
	switch actionType {
	case domain.ActionMute:
		return []enforcementStep{enforceMute}
	case domain.ActionSuspend:
		return []enforcementStep{enforceDeactivate}
	case domain.ActionBan:
		return []enforcementStep{enforceBanRole, enforceZeroTrust}
	default:
		return nil
	}
}

// ModerationActionLister reads the moderation audit trail. The history service
// needs only these two, so it takes this rather than the full repository.
type ModerationActionLister interface {
	ListActionsByTarget(ctx context.Context, targetUserID string, limit, offset int) ([]*domain.ModerationAction, error)
	ListActionsByModerator(ctx context.Context, moderatorID string, limit, offset int) ([]*domain.ModerationAction, error)
}

// ModerationActionRepository abstracts moderation action persistence.
type ModerationActionRepository interface {
	CreateModerationAction(ctx context.Context, action *domain.ModerationAction) error
	ModerationActionLister
}

// PenaltyPropagator applies the trust penalties a moderation action implies.
//
// ModerationActionService depends on this rather than on *ModerationService so
// that it names the one method it actually calls: constructing the concrete
// service drags in a penalty repository and a vouch graph, which callers that
// only take actions have no reason to supply.
type PenaltyPropagator interface {
	PropagatePenalties(ctx context.Context, actionID, targetUserID string, severity int) ([]domain.TrustPenalty, error)
}

// PenaltyLister extends PenaltyRepository with read operations for audit.
type PenaltyLister interface {
	PenaltyRepository
	ListPenaltiesByActionID(ctx context.Context, actionID string) ([]domain.TrustPenalty, error)
}

// ActionUserLookup retrieves a user by ID for moderation action validation.
type ActionUserLookup interface {
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
}

// TakeActionResult holds the created action and any penalties applied.
type TakeActionResult struct {
	Action    *domain.ModerationAction `json:"action"`
	Penalties []domain.TrustPenalty    `json:"penalties"`
}

// UserEnforcer applies immediate user state changes for moderation actions.
type UserEnforcer interface {
	DeactivateUser(ctx context.Context, id string) error
	UpdateUserRole(ctx context.Context, id string, role domain.Role) error
	UpdateUserTrustScore(ctx context.Context, id string, score float64) error
	SetUserMutedUntil(ctx context.Context, id string, until *time.Time) error
}

// ActionHistoryEntry pairs a moderation action with its trust penalties.
type ActionHistoryEntry struct {
	Action    *domain.ModerationAction `json:"action"`
	Penalties []domain.TrustPenalty    `json:"penalties"`
}

// ModerationActionService orchestrates moderation action business logic.
type ModerationActionService struct {
	actions    ModerationActionRepository
	users      ActionUserLookup
	moderation PenaltyPropagator
	enforcer   UserEnforcer
	history    *ModerationHistoryService
	now        func() time.Time
}

func NewModerationActionService(
	actions ModerationActionRepository,
	users ActionUserLookup,
	moderation PenaltyPropagator,
	enforcer UserEnforcer,
	penalties PenaltyLister,
	clock func() time.Time,
) *ModerationActionService {
	if clock == nil {
		clock = time.Now
	}
	return &ModerationActionService{
		actions:    actions,
		users:      users,
		moderation: moderation,
		enforcer:   enforcer,
		history:    NewModerationHistoryService(actions, penalties),
		now:        clock,
	}
}

// TakeAction creates a moderation action and triggers trust penalty propagation.
func (s *ModerationActionService) TakeAction(
	ctx context.Context,
	moderatorID, targetUserID string,
	actionType domain.ActionType,
	severity int,
	reason string,
	durationSeconds *int64,
) (*TakeActionResult, error) {
	req, err := validateActionRequest(moderatorID, targetUserID, actionType, severity, reason, durationSeconds)
	if err != nil {
		return nil, err
	}

	// Verify target user exists and capture for enforcement.
	targetUser, err := s.users.GetUserByID(ctx, req.TargetUserID)
	if err != nil {
		return nil, err
	}

	now := s.now()

	var expiresAt *time.Time
	if req.Duration != nil {
		t := now.Add(*req.Duration)
		expiresAt = &t
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generating action id: %w", err)
	}

	action := &domain.ModerationAction{
		ID:           id.String(),
		TargetUserID: req.TargetUserID,
		ModeratorID:  req.ModeratorID,
		Action:       req.ActionType,
		Severity:     req.Severity,
		Reason:       req.Reason,
		Duration:     req.Duration,
		CreatedAt:    now,
		ExpiresAt:    expiresAt,
	}

	if err := s.actions.CreateModerationAction(ctx, action); err != nil {
		return nil, fmt.Errorf("creating moderation action: %w", err)
	}

	// Enforcement runs before propagation and deliberately does not depend on
	// the vouch graph: it needs only the action type and the user. Running it
	// second meant an unreachable graph returned early and left a banned user
	// un-banned and still able to post.
	enforceErr := s.enforce(ctx, req.ActionType, targetUser, expiresAt)
	if enforceErr != nil {
		enforceErr = fmt.Errorf("enforcing action: %w", enforceErr)
	}

	penalties, propagateErr := s.moderation.PropagatePenalties(ctx, action.ID, req.TargetUserID, req.Severity)
	if propagateErr != nil {
		propagateErr = fmt.Errorf("propagating penalties: %w", propagateErr)
	}

	// Action was persisted; return a partial result alongside any error so the
	// caller does not retry and duplicate it.
	result := &TakeActionResult{Action: action, Penalties: penalties}
	return result, errors.Join(enforceErr, propagateErr)
}

// GetActionHistory delegates to ModerationHistoryService.
//
// Reading the audit trail shares nothing with taking an action, so the logic
// lives on its own type. This method remains only because the moderation
// handler and cmd/bell reach for it through *ModerationActionService; once
// those depend on ModerationHistoryService directly it should be deleted.
func (s *ModerationActionService) GetActionHistory(
	ctx context.Context,
	userID string,
	byModerator bool,
	limit, offset int,
) ([]ActionHistoryEntry, error) {
	return s.history.GetActionHistory(ctx, userID, byModerator, limit, offset)
}

// ModerationHistoryService reads the moderation audit trail: the actions
// recorded against (or by) a user, each paired with the trust penalties it
// caused. It holds no clock, no enforcer and no penalty engine, because
// reporting on past moderation decides nothing.
type ModerationHistoryService struct {
	actions   ModerationActionLister
	penalties PenaltyLister
}

func NewModerationHistoryService(actions ModerationActionLister, penalties PenaltyLister) *ModerationHistoryService {
	return &ModerationHistoryService{actions: actions, penalties: penalties}
}

// GetActionHistory returns moderation actions with their associated penalties.
// If byModerator is true, it lists actions taken BY the user (for council audit).
// Otherwise, it lists actions taken AGAINST the user.
func (s *ModerationHistoryService) GetActionHistory(
	ctx context.Context,
	userID string,
	byModerator bool,
	limit, offset int,
) ([]ActionHistoryEntry, error) {
	var actions []*domain.ModerationAction
	var err error

	if byModerator {
		actions, err = s.actions.ListActionsByModerator(ctx, userID, limit, offset)
	} else {
		actions, err = s.actions.ListActionsByTarget(ctx, userID, limit, offset)
	}
	if err != nil {
		return nil, fmt.Errorf("listing moderation actions: %w", err)
	}

	entries := make([]ActionHistoryEntry, 0, len(actions))
	// TODO: batch query — currently N+1, acceptable at pagination limits (~20)
	for _, action := range actions {
		var penalties []domain.TrustPenalty
		if s.penalties != nil {
			penalties, err = s.penalties.ListPenaltiesByActionID(ctx, action.ID)
			if err != nil {
				return nil, fmt.Errorf("listing penalties for action %s: %w", action.ID, err)
			}
		}
		if penalties == nil {
			penalties = []domain.TrustPenalty{}
		}
		entries = append(entries, ActionHistoryEntry{
			Action:    action,
			Penalties: penalties,
		})
	}

	return entries, nil
}

// enforce applies the state changes planned by planEnforcement.
//
// expiresAt is the action's own expiry, computed once in TakeAction from the
// duration the moderator chose. Reusing it rather than recomputing keeps the
// action row's expires_at and the user's muted_until from ever disagreeing.
func (s *ModerationActionService) enforce(ctx context.Context, actionType domain.ActionType, user *domain.User, expiresAt *time.Time) error {
	if s.enforcer == nil {
		return nil
	}

	for _, step := range planEnforcement(actionType) {
		var err error
		switch step {
		case enforceMute:
			err = s.enforcer.SetUserMutedUntil(ctx, user.ID, expiresAt)
		case enforceDeactivate:
			err = s.enforcer.DeactivateUser(ctx, user.ID)
		case enforceBanRole:
			err = s.enforcer.UpdateUserRole(ctx, user.ID, domain.RoleBanned)
		case enforceZeroTrust:
			err = s.enforcer.UpdateUserTrustScore(ctx, user.ID, 0)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

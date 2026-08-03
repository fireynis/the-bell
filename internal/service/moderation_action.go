package service

import (
	"context"
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
	if (actionType == domain.ActionMute || actionType == domain.ActionSuspend) && durationSeconds == nil {
		return actionRequest{}, fmt.Errorf("%w: %s requires a duration", ErrValidation, actionType)
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
	// enforceDropBelowPostingThreshold silences a user by pushing their trust
	// just under the posting threshold.
	enforceDropBelowPostingThreshold enforcementStep = iota
	// enforceDeactivate suspends the account.
	enforceDeactivate
	// enforceBanRole moves the user to the banned role.
	enforceBanRole
	// enforceZeroTrust wipes the trust score.
	enforceZeroTrust
)

// planEnforcement decides which immediate state changes an action implies.
//
// A mute only needs to act when the user is currently above the posting
// threshold: someone already below it cannot post, and lowering their score
// further would stack extra punishment on top of the trust penalty.
func planEnforcement(actionType domain.ActionType, user *domain.User) []enforcementStep {
	switch actionType {
	case domain.ActionMute:
		if user != nil && user.TrustScore >= domain.PostingThreshold {
			return []enforcementStep{enforceDropBelowPostingThreshold}
		}
		return nil
	case domain.ActionSuspend:
		return []enforcementStep{enforceDeactivate}
	case domain.ActionBan:
		return []enforcementStep{enforceBanRole, enforceZeroTrust}
	default:
		return nil
	}
}

// ModerationActionRepository abstracts moderation action persistence.
type ModerationActionRepository interface {
	CreateModerationAction(ctx context.Context, action *domain.ModerationAction) error
	ListActionsByTarget(ctx context.Context, targetUserID string, limit, offset int) ([]*domain.ModerationAction, error)
	ListActionsByModerator(ctx context.Context, moderatorID string, limit, offset int) ([]*domain.ModerationAction, error)
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
	moderation *ModerationService
	enforcer   UserEnforcer
	penalties  PenaltyLister
	now        func() time.Time
}

func NewModerationActionService(
	actions ModerationActionRepository,
	users ActionUserLookup,
	moderation *ModerationService,
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
		penalties:  penalties,
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

	penalties, err := s.moderation.PropagatePenalties(ctx, action.ID, req.TargetUserID, req.Severity)
	if err != nil {
		// Action was persisted; return partial result with error.
		return &TakeActionResult{Action: action, Penalties: penalties}, fmt.Errorf("propagating penalties: %w", err)
	}

	result := &TakeActionResult{Action: action, Penalties: penalties}

	if err := s.enforce(ctx, req.ActionType, targetUser); err != nil {
		return result, fmt.Errorf("enforcing action: %w", err)
	}

	return result, nil
}

// GetActionHistory returns moderation actions with their associated penalties.
// If byModerator is true, it lists actions taken BY the user (for council audit).
// Otherwise, it lists actions taken AGAINST the user.
func (s *ModerationActionService) GetActionHistory(
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
func (s *ModerationActionService) enforce(ctx context.Context, actionType domain.ActionType, user *domain.User) error {
	if s.enforcer == nil {
		return nil
	}

	for _, step := range planEnforcement(actionType, user) {
		var err error
		switch step {
		case enforceDropBelowPostingThreshold:
			err = s.enforcer.UpdateUserTrustScore(ctx, user.ID, domain.PostingThreshold-1.0)
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

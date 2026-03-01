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
	// Validate action type
	allowed, ok := allowedSeverity[actionType]
	if !ok {
		return nil, fmt.Errorf("%w: invalid action type %q", ErrValidation, actionType)
	}

	// Validate severity matches action type
	if !slices.Contains(allowed, severity) {
		return nil, fmt.Errorf("%w: severity %d not valid for action type %q", ErrValidation, severity, actionType)
	}

	// Validate reason
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: reason must not be empty", ErrValidation)
	}
	if len(reason) > maxActionReasonLen {
		return nil, fmt.Errorf("%w: reason exceeds %d characters", ErrValidation, maxActionReasonLen)
	}

	// Prevent self-moderation
	if moderatorID == targetUserID {
		return nil, fmt.Errorf("%w: cannot moderate yourself", ErrValidation)
	}

	// Verify target user exists and capture for enforcement
	targetUser, err := s.users.GetUserByID(ctx, targetUserID)
	if err != nil {
		return nil, err
	}

	// Validate duration rules
	if actionType == domain.ActionBan && durationSeconds != nil {
		return nil, fmt.Errorf("%w: bans cannot have a duration", ErrValidation)
	}
	if (actionType == domain.ActionMute || actionType == domain.ActionSuspend) && durationSeconds == nil {
		return nil, fmt.Errorf("%w: %s requires a duration", ErrValidation, actionType)
	}

	now := s.now()

	// Compute expires_at
	var expiresAt *time.Time
	var dur *time.Duration
	if durationSeconds != nil {
		d := time.Duration(*durationSeconds) * time.Second
		dur = &d
		t := now.Add(d)
		expiresAt = &t
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generating action id: %w", err)
	}

	action := &domain.ModerationAction{
		ID:           id.String(),
		TargetUserID: targetUserID,
		ModeratorID:  moderatorID,
		Action:       actionType,
		Severity:     severity,
		Reason:       reason,
		Duration:     dur,
		CreatedAt:    now,
		ExpiresAt:    expiresAt,
	}

	if err := s.actions.CreateModerationAction(ctx, action); err != nil {
		return nil, fmt.Errorf("creating moderation action: %w", err)
	}

	penalties, err := s.moderation.PropagatePenalties(ctx, action.ID, targetUserID, severity)
	if err != nil {
		// Action was persisted; return partial result with error.
		return &TakeActionResult{Action: action, Penalties: penalties}, fmt.Errorf("propagating penalties: %w", err)
	}

	result := &TakeActionResult{Action: action, Penalties: penalties}

	if err := s.enforce(ctx, actionType, targetUser); err != nil {
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

func (s *ModerationActionService) enforce(ctx context.Context, actionType domain.ActionType, user *domain.User) error {
	if s.enforcer == nil {
		return nil
	}

	switch actionType {
	case domain.ActionMute:
		if user.TrustScore >= domain.PostingThreshold {
			return s.enforcer.UpdateUserTrustScore(ctx, user.ID, domain.PostingThreshold-1.0)
		}
	case domain.ActionSuspend:
		return s.enforcer.DeactivateUser(ctx, user.ID)
	case domain.ActionBan:
		if err := s.enforcer.UpdateUserRole(ctx, user.ID, domain.RoleBanned); err != nil {
			return err
		}
		return s.enforcer.UpdateUserTrustScore(ctx, user.ID, 0)
	}
	return nil
}

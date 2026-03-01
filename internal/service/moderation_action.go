package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/google/uuid"
)

const maxActionReasonLen = 1000

// AllowedSeverity maps each action type to its valid severity values.
var AllowedSeverity = map[domain.ActionType][]int{
	domain.ActionWarn:    {1, 2},
	domain.ActionMute:    {3},
	domain.ActionSuspend: {4},
	domain.ActionBan:     {5},
}

// ModerationActionRepository abstracts moderation action persistence.
type ModerationActionRepository interface {
	CreateModerationAction(ctx context.Context, action *domain.ModerationAction) error
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

// ModerationActionService orchestrates moderation action business logic.
type ModerationActionService struct {
	actions    ModerationActionRepository
	users      ActionUserLookup
	moderation *ModerationService
	now        func() time.Time
}

func NewModerationActionService(
	actions ModerationActionRepository,
	users ActionUserLookup,
	moderation *ModerationService,
	clock func() time.Time,
) *ModerationActionService {
	if clock == nil {
		clock = time.Now
	}
	return &ModerationActionService{
		actions:    actions,
		users:      users,
		moderation: moderation,
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
	allowed, ok := AllowedSeverity[actionType]
	if !ok {
		return nil, fmt.Errorf("%w: invalid action type %q", ErrValidation, actionType)
	}

	// Validate severity matches action type
	if !containsInt(allowed, severity) {
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

	// Verify target user exists
	if _, err := s.users.GetUserByID(ctx, targetUserID); err != nil {
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

	return &TakeActionResult{Action: action, Penalties: penalties}, nil
}

func containsInt(slice []int, val int) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
}

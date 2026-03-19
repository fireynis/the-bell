package service

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// mockRoleCheckerRepo is an in-memory RoleCheckerRepository for testing.
type mockRoleCheckerRepo struct {
	users             []RoleCheckerUser
	modVouchCounts    map[string]int64
	updatedRoles      map[string]domain.Role
	trustBelowSince   map[string]*time.Time
	clearedUsers      map[string]bool
	roleHistoryEvents []domain.RoleHistory

	updateRoleErr           error
	updateTrustBelowErr     error
	clearTrustBelowErr      error
	countModVouchesErr      error
	createRoleHistoryErr    error
	listUsersErr            error
}

func newMockRoleCheckerRepo() *mockRoleCheckerRepo {
	return &mockRoleCheckerRepo{
		modVouchCounts:  make(map[string]int64),
		updatedRoles:    make(map[string]domain.Role),
		trustBelowSince: make(map[string]*time.Time),
		clearedUsers:    make(map[string]bool),
	}
}

func (m *mockRoleCheckerRepo) ListActiveNonBannedUsers(_ context.Context) ([]RoleCheckerUser, error) {
	if m.listUsersErr != nil {
		return nil, m.listUsersErr
	}
	return m.users, nil
}

func (m *mockRoleCheckerRepo) CountActiveModeratorVouchesForUser(_ context.Context, userID string) (int64, error) {
	if m.countModVouchesErr != nil {
		return 0, m.countModVouchesErr
	}
	return m.modVouchCounts[userID], nil
}

func (m *mockRoleCheckerRepo) UpdateUserRole(_ context.Context, id string, role domain.Role) error {
	if m.updateRoleErr != nil {
		return m.updateRoleErr
	}
	m.updatedRoles[id] = role
	return nil
}

func (m *mockRoleCheckerRepo) UpdateUserTrustBelowSince(_ context.Context, id string, since time.Time) error {
	if m.updateTrustBelowErr != nil {
		return m.updateTrustBelowErr
	}
	m.trustBelowSince[id] = &since
	return nil
}

func (m *mockRoleCheckerRepo) ClearUserTrustBelowSince(_ context.Context, id string) error {
	if m.clearTrustBelowErr != nil {
		return m.clearTrustBelowErr
	}
	m.clearedUsers[id] = true
	delete(m.trustBelowSince, id)
	return nil
}

func (m *mockRoleCheckerRepo) CreateRoleHistoryEntry(_ context.Context, entry *domain.RoleHistory) error {
	if m.createRoleHistoryErr != nil {
		return m.createRoleHistoryErr
	}
	m.roleHistoryEvents = append(m.roleHistoryEvents, *entry)
	return nil
}

var roleCheckNow = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func roleCheckClock() time.Time { return roleCheckNow }

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func TestRoleChecker_Run(t *testing.T) {
	thirtyOneDaysAgo := roleCheckNow.AddDate(0, 0, -31)
	twentyNineDaysAgo := roleCheckNow.AddDate(0, 0, -29)
	ninetyOneDaysAgo := roleCheckNow.AddDate(0, 0, -91)
	exactlyThirtyDaysAgo := roleCheckNow.AddDate(0, 0, -30)
	exactlyNinetyDaysAgo := roleCheckNow.AddDate(0, 0, -90)

	tests := []struct {
		name            string
		users           []RoleCheckerUser
		modVouchCounts  map[string]int64
		wantPromotions  int
		wantDemotions   int
		wantCleared     int
		wantMarked      int
		wantRoles       map[string]domain.Role
	}{
		{
			name: "member promoted to moderator",
			users: []RoleCheckerUser{
				{
					ID:          "user-1",
					DisplayName: "Alice",
					TrustScore:  90.0,
					Role:        domain.RoleMember,
					JoinedAt:    roleCheckNow.AddDate(0, 0, -100),
				},
			},
			modVouchCounts: map[string]int64{"user-1": 3},
			wantPromotions: 1,
			wantDemotions:  0,
			wantCleared:    0,
			wantMarked:     0,
			wantRoles:      map[string]domain.Role{"user-1": domain.RoleModerator},
		},
		{
			name: "member trust exactly at promotion threshold",
			users: []RoleCheckerUser{
				{
					ID:          "user-1",
					DisplayName: "Alice",
					TrustScore:  85.0,
					Role:        domain.RoleMember,
					JoinedAt:    roleCheckNow.AddDate(0, 0, -100),
				},
			},
			modVouchCounts: map[string]int64{"user-1": 2},
			wantPromotions: 1,
			wantDemotions:  0,
			wantCleared:    0,
			wantMarked:     0,
			wantRoles:      map[string]domain.Role{"user-1": domain.RoleModerator},
		},
		{
			name: "member trust just below promotion threshold",
			users: []RoleCheckerUser{
				{
					ID:          "user-1",
					DisplayName: "Alice",
					TrustScore:  84.9,
					Role:        domain.RoleMember,
					JoinedAt:    roleCheckNow.AddDate(0, 0, -100),
				},
			},
			modVouchCounts: map[string]int64{"user-1": 3},
			wantPromotions: 0,
			wantDemotions:  0,
			wantCleared:    0,
			wantMarked:     0,
			wantRoles:      map[string]domain.Role{},
		},
		{
			name: "member not enough days",
			users: []RoleCheckerUser{
				{
					ID:          "user-1",
					DisplayName: "Alice",
					TrustScore:  90.0,
					Role:        domain.RoleMember,
					JoinedAt:    roleCheckNow.AddDate(0, 0, -50),
				},
			},
			modVouchCounts: map[string]int64{"user-1": 3},
			wantPromotions: 0,
			wantDemotions:  0,
			wantCleared:    0,
			wantMarked:     0,
			wantRoles:      map[string]domain.Role{},
		},
		{
			name: "member joined exactly 90 days ago",
			users: []RoleCheckerUser{
				{
					ID:          "user-1",
					DisplayName: "Alice",
					TrustScore:  90.0,
					Role:        domain.RoleMember,
					JoinedAt:    exactlyNinetyDaysAgo,
				},
			},
			modVouchCounts: map[string]int64{"user-1": 2},
			wantPromotions: 1,
			wantDemotions:  0,
			wantCleared:    0,
			wantMarked:     0,
			wantRoles:      map[string]domain.Role{"user-1": domain.RoleModerator},
		},
		{
			name: "member not enough moderator vouches",
			users: []RoleCheckerUser{
				{
					ID:          "user-1",
					DisplayName: "Alice",
					TrustScore:  90.0,
					Role:        domain.RoleMember,
					JoinedAt:    roleCheckNow.AddDate(0, 0, -100),
				},
			},
			modVouchCounts: map[string]int64{"user-1": 1},
			wantPromotions: 0,
			wantDemotions:  0,
			wantCleared:    0,
			wantMarked:     0,
			wantRoles:      map[string]domain.Role{},
		},
		{
			name: "member trust below 70 first time - mark TrustBelowSince",
			users: []RoleCheckerUser{
				{
					ID:              "user-1",
					DisplayName:     "Bob",
					TrustScore:      65.0,
					Role:            domain.RoleMember,
					JoinedAt:        roleCheckNow.AddDate(0, 0, -100),
					TrustBelowSince: nil,
				},
			},
			wantPromotions: 0,
			wantDemotions:  0,
			wantCleared:    0,
			wantMarked:     1,
			wantRoles:      map[string]domain.Role{},
		},
		{
			name: "member trust below 70 for 30+ days - demote to pending",
			users: []RoleCheckerUser{
				{
					ID:              "user-1",
					DisplayName:     "Bob",
					TrustScore:      65.0,
					Role:            domain.RoleMember,
					JoinedAt:        roleCheckNow.AddDate(0, 0, -100),
					TrustBelowSince: &thirtyOneDaysAgo,
				},
			},
			wantPromotions: 0,
			wantDemotions:  1,
			wantCleared:    0,
			wantMarked:     0,
			wantRoles:      map[string]domain.Role{"user-1": domain.RolePending},
		},
		{
			name: "moderator trust below 70 for 30+ days - demote to member",
			users: []RoleCheckerUser{
				{
					ID:              "mod-1",
					DisplayName:     "Charlie",
					TrustScore:      60.0,
					Role:            domain.RoleModerator,
					JoinedAt:        roleCheckNow.AddDate(0, 0, -200),
					TrustBelowSince: &thirtyOneDaysAgo,
				},
			},
			wantPromotions: 0,
			wantDemotions:  1,
			wantCleared:    0,
			wantMarked:     0,
			wantRoles:      map[string]domain.Role{"mod-1": domain.RoleMember},
		},
		{
			name: "member trust below 70 for exactly 30 days - demote",
			users: []RoleCheckerUser{
				{
					ID:              "user-1",
					DisplayName:     "Bob",
					TrustScore:      65.0,
					Role:            domain.RoleMember,
					JoinedAt:        roleCheckNow.AddDate(0, 0, -100),
					TrustBelowSince: &exactlyThirtyDaysAgo,
				},
			},
			wantPromotions: 0,
			wantDemotions:  1,
			wantCleared:    0,
			wantMarked:     0,
			wantRoles:      map[string]domain.Role{"user-1": domain.RolePending},
		},
		{
			name: "member trust below 70 for just under 30 days - no demotion",
			users: []RoleCheckerUser{
				{
					ID:              "user-1",
					DisplayName:     "Bob",
					TrustScore:      65.0,
					Role:            domain.RoleMember,
					JoinedAt:        roleCheckNow.AddDate(0, 0, -100),
					TrustBelowSince: &twentyNineDaysAgo,
				},
			},
			wantPromotions: 0,
			wantDemotions:  0,
			wantCleared:    0,
			wantMarked:     0,
			wantRoles:      map[string]domain.Role{},
		},
		{
			name: "user trust recovered above 70 - clear TrustBelowSince",
			users: []RoleCheckerUser{
				{
					ID:              "user-1",
					DisplayName:     "Dave",
					TrustScore:      75.0,
					Role:            domain.RoleMember,
					JoinedAt:        roleCheckNow.AddDate(0, 0, -100),
					TrustBelowSince: &thirtyOneDaysAgo,
				},
			},
			wantPromotions: 0,
			wantDemotions:  0,
			wantCleared:    1,
			wantMarked:     0,
			wantRoles:      map[string]domain.Role{},
		},
		{
			name: "user trust exactly at demotion threshold - no demotion, clear marker",
			users: []RoleCheckerUser{
				{
					ID:              "user-1",
					DisplayName:     "Eve",
					TrustScore:      70.0,
					Role:            domain.RoleMember,
					JoinedAt:        roleCheckNow.AddDate(0, 0, -100),
					TrustBelowSince: &thirtyOneDaysAgo,
				},
			},
			wantPromotions: 0,
			wantDemotions:  0,
			wantCleared:    1,
			wantMarked:     0,
			wantRoles:      map[string]domain.Role{},
		},
		{
			name: "council member is never auto-promoted or demoted",
			users: []RoleCheckerUser{
				{
					ID:              "council-1",
					DisplayName:     "Frank",
					TrustScore:      50.0,
					Role:            domain.RoleCouncil,
					JoinedAt:        roleCheckNow.AddDate(-2, 0, 0),
					TrustBelowSince: &thirtyOneDaysAgo,
				},
			},
			wantPromotions: 0,
			wantDemotions:  0,
			wantCleared:    0,
			wantMarked:     0,
			wantRoles:      map[string]domain.Role{},
		},
		{
			name: "multiple users - mixed promotions and demotions",
			users: []RoleCheckerUser{
				{
					ID:          "promotable",
					DisplayName: "Alice",
					TrustScore:  90.0,
					Role:        domain.RoleMember,
					JoinedAt:    ninetyOneDaysAgo,
				},
				{
					ID:              "demotable-member",
					DisplayName:     "Bob",
					TrustScore:      65.0,
					Role:            domain.RoleMember,
					JoinedAt:        roleCheckNow.AddDate(0, 0, -100),
					TrustBelowSince: &thirtyOneDaysAgo,
				},
				{
					ID:              "demotable-mod",
					DisplayName:     "Charlie",
					TrustScore:      60.0,
					Role:            domain.RoleModerator,
					JoinedAt:        roleCheckNow.AddDate(0, 0, -200),
					TrustBelowSince: &thirtyOneDaysAgo,
				},
				{
					ID:          "council-safe",
					DisplayName: "Diana",
					TrustScore:  40.0,
					Role:        domain.RoleCouncil,
					JoinedAt:    roleCheckNow.AddDate(-2, 0, 0),
				},
			},
			modVouchCounts: map[string]int64{"promotable": 2},
			wantPromotions: 1,
			wantDemotions:  2,
			wantCleared:    0,
			wantMarked:     0,
			wantRoles: map[string]domain.Role{
				"promotable":      domain.RoleModerator,
				"demotable-member": domain.RolePending,
				"demotable-mod":    domain.RoleMember,
			},
		},
		{
			name:           "no users to check",
			users:          []RoleCheckerUser{},
			wantPromotions: 0,
			wantDemotions:  0,
			wantCleared:    0,
			wantMarked:     0,
			wantRoles:      map[string]domain.Role{},
		},
		{
			name: "trust exactly at 70 with no TrustBelowSince - no action needed",
			users: []RoleCheckerUser{
				{
					ID:          "user-1",
					DisplayName: "Grace",
					TrustScore:  70.0,
					Role:        domain.RoleMember,
					JoinedAt:    roleCheckNow.AddDate(0, 0, -100),
				},
			},
			wantPromotions: 0,
			wantDemotions:  0,
			wantCleared:    0,
			wantMarked:     0,
			wantRoles:      map[string]domain.Role{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockRoleCheckerRepo()
			repo.users = tt.users
			if tt.modVouchCounts != nil {
				repo.modVouchCounts = tt.modVouchCounts
			}

			rc := NewRoleChecker(repo, testLogger(), roleCheckClock)
			result, err := rc.Run(context.Background())
			if err != nil {
				t.Fatalf("Run() unexpected error: %v", err)
			}

			if result.UsersChecked != len(tt.users) {
				t.Errorf("UsersChecked = %d, want %d", result.UsersChecked, len(tt.users))
			}

			if len(result.Promotions) != tt.wantPromotions {
				t.Errorf("Promotions = %d, want %d", len(result.Promotions), tt.wantPromotions)
			}
			if len(result.Demotions) != tt.wantDemotions {
				t.Errorf("Demotions = %d, want %d", len(result.Demotions), tt.wantDemotions)
			}
			if result.Cleared != tt.wantCleared {
				t.Errorf("Cleared = %d, want %d", result.Cleared, tt.wantCleared)
			}
			if result.Marked != tt.wantMarked {
				t.Errorf("Marked = %d, want %d", result.Marked, tt.wantMarked)
			}

			wantRoles := tt.wantRoles
			if wantRoles == nil {
				wantRoles = map[string]domain.Role{}
			}
			if len(repo.updatedRoles) != len(wantRoles) {
				t.Errorf("updated roles count = %d, want %d", len(repo.updatedRoles), len(wantRoles))
			}
			for id, wantRole := range wantRoles {
				if got, ok := repo.updatedRoles[id]; !ok {
					t.Errorf("role not updated for user %q", id)
				} else if got != wantRole {
					t.Errorf("user %q role = %q, want %q", id, got, wantRole)
				}
			}

			// Verify role_history entries match promotions + demotions
			wantHistoryCount := tt.wantPromotions + tt.wantDemotions
			if len(repo.roleHistoryEvents) != wantHistoryCount {
				t.Errorf("role_history entries = %d, want %d", len(repo.roleHistoryEvents), wantHistoryCount)
			}
		})
	}
}

func TestRoleChecker_Run_ListUsersError(t *testing.T) {
	repo := newMockRoleCheckerRepo()
	repo.listUsersErr = ErrNotFound

	rc := NewRoleChecker(repo, testLogger(), roleCheckClock)
	_, err := rc.Run(context.Background())
	if err == nil {
		t.Fatal("Run() expected error, got nil")
	}
}

func TestRoleChecker_DemotionClearsTrustBelowSince(t *testing.T) {
	thirtyOneDaysAgo := roleCheckNow.AddDate(0, 0, -31)
	repo := newMockRoleCheckerRepo()
	repo.users = []RoleCheckerUser{
		{
			ID:              "user-1",
			DisplayName:     "Bob",
			TrustScore:      60.0,
			Role:            domain.RoleMember,
			JoinedAt:        roleCheckNow.AddDate(0, 0, -100),
			TrustBelowSince: &thirtyOneDaysAgo,
		},
	}

	rc := NewRoleChecker(repo, testLogger(), roleCheckClock)
	result, err := rc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if len(result.Demotions) != 1 {
		t.Fatalf("Demotions = %d, want 1", len(result.Demotions))
	}

	// After demotion, TrustBelowSince should be cleared.
	if !repo.clearedUsers["user-1"] {
		t.Error("TrustBelowSince not cleared after demotion")
	}
}

func TestRoleChecker_PromotionRecordsHistory(t *testing.T) {
	repo := newMockRoleCheckerRepo()
	repo.users = []RoleCheckerUser{
		{
			ID:          "user-1",
			DisplayName: "Alice",
			TrustScore:  90.0,
			Role:        domain.RoleMember,
			JoinedAt:    roleCheckNow.AddDate(0, 0, -100),
		},
	}
	repo.modVouchCounts = map[string]int64{"user-1": 3}

	rc := NewRoleChecker(repo, testLogger(), roleCheckClock)
	_, err := rc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if len(repo.roleHistoryEvents) != 1 {
		t.Fatalf("role_history entries = %d, want 1", len(repo.roleHistoryEvents))
	}

	entry := repo.roleHistoryEvents[0]
	if entry.UserID != "user-1" {
		t.Errorf("role_history user_id = %q, want %q", entry.UserID, "user-1")
	}
	if entry.OldRole != domain.RoleMember {
		t.Errorf("role_history old_role = %q, want %q", entry.OldRole, domain.RoleMember)
	}
	if entry.NewRole != domain.RoleModerator {
		t.Errorf("role_history new_role = %q, want %q", entry.NewRole, domain.RoleModerator)
	}
	if entry.ID == "" {
		t.Error("role_history ID should not be empty")
	}
	if entry.Reason == "" {
		t.Error("role_history reason should not be empty")
	}
}

func TestRoleChecker_NilClock(t *testing.T) {
	repo := newMockRoleCheckerRepo()
	repo.users = []RoleCheckerUser{}

	rc := NewRoleChecker(repo, testLogger(), nil)
	result, err := rc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if result.UsersChecked != 0 {
		t.Errorf("UsersChecked = %d, want 0", result.UsersChecked)
	}
}

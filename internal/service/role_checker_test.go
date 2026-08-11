package service

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// mockRoleCheckerRepo is an in-memory RoleCheckerRepository for testing.
type mockRoleCheckerRepo struct {
	users             []*domain.User
	modVouchCounts    map[string]int64
	updatedRoles      map[string]domain.Role
	trustBelowSince   map[string]*time.Time
	clearedUsers      map[string]bool
	roleHistoryEvents []domain.RoleHistory

	updateRoleErr        error
	updateTrustBelowErr  error
	clearTrustBelowErr   error
	countModVouchesErr   error
	createRoleHistoryErr error
	listUsersErr         error
}

func newMockRoleCheckerRepo() *mockRoleCheckerRepo {
	return &mockRoleCheckerRepo{
		modVouchCounts:  make(map[string]int64),
		updatedRoles:    make(map[string]domain.Role),
		trustBelowSince: make(map[string]*time.Time),
		clearedUsers:    make(map[string]bool),
	}
}

func (m *mockRoleCheckerRepo) ListActiveNonBannedUsers(_ context.Context) ([]*domain.User, error) {
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

	tests := []struct {
		name           string
		users          []*domain.User
		modVouchCounts map[string]int64
		wantPromotions int
		wantDemotions  int
		wantCleared    int
		wantMarked     int
		wantRoles      map[string]domain.Role
	}{
		{
			name: "one pass applies every outcome across a mixed population",
			users: []*domain.User{
				{
					ID:          "promotable",
					DisplayName: "Alice",
					TrustScore:  90.0,
					Role:        domain.RoleMember,
					JoinedAt:    ninetyOneDaysAgo,
				},
				{
					// First dip below the threshold: starts the demotion clock.
					ID:          "freshly-dipped",
					DisplayName: "Erin",
					TrustScore:  65.0,
					Role:        domain.RoleMember,
					JoinedAt:    roleCheckNow.AddDate(0, 0, -100),
				},
				{
					// Recovered before the window elapsed: the clock is cleared.
					// No moderator vouches, so this must not also promote.
					ID:              "recovered",
					DisplayName:     "Grace",
					TrustScore:      90.0,
					Role:            domain.RoleMember,
					JoinedAt:        roleCheckNow.AddDate(0, 0, -100),
					TrustBelowSince: &twentyNineDaysAgo,
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
			wantCleared:    1,
			wantMarked:     1,
			wantRoles: map[string]domain.Role{
				"promotable":       domain.RoleModerator,
				"demotable-member": domain.RolePending,
				"demotable-mod":    domain.RoleMember,
			},
		},
		{
			name:           "no users to check",
			users:          []*domain.User{},
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
	repo.users = []*domain.User{
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
	repo.users = []*domain.User{
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

// Demotion is the only outcome that takes something away, so it must be
// reached explicitly. An outcome the switch does not recognize — which is what
// a newly added demotionOutcome looks like before its case is written — has to
// fail loudly rather than fall through into a role change.
func TestRoleChecker_ApplyDemotion_UnrecognizedOutcomeDoesNotDemote(t *testing.T) {
	repo := newMockRoleCheckerRepo()
	rc := NewRoleChecker(repo, testLogger(), roleCheckClock)
	u := &domain.User{ID: "user-1", DisplayName: "Bob", TrustScore: 10, Role: domain.RoleMember}
	result := &RoleCheckResult{}

	decision := demotionDecision{Outcome: demotionOutcome(99), NewRole: domain.RolePending, Reason: "should not apply"}
	err := rc.applyDemotion(context.Background(), u, decision, roleCheckNow, result)
	if err == nil {
		t.Fatal("applyDemotion() with an unrecognized outcome: expected an error, got nil")
	}
	if len(repo.updatedRoles) != 0 {
		t.Errorf("roles updated = %v, want none", repo.updatedRoles)
	}
	if len(repo.roleHistoryEvents) != 0 {
		t.Errorf("role_history entries = %d, want 0", len(repo.roleHistoryEvents))
	}
	if len(result.Demotions) != 0 {
		t.Errorf("Demotions = %d, want 0", len(result.Demotions))
	}
}

// demotionNone is the zero value, so a decision that was never populated must
// be a no-op rather than the most destructive outcome.
func TestRoleChecker_ApplyDemotion_ZeroDecisionIsANoOp(t *testing.T) {
	repo := newMockRoleCheckerRepo()
	rc := NewRoleChecker(repo, testLogger(), roleCheckClock)
	u := &domain.User{ID: "user-1", DisplayName: "Bob", TrustScore: 10, Role: domain.RoleMember}
	result := &RoleCheckResult{}

	if err := rc.applyDemotion(context.Background(), u, demotionDecision{}, roleCheckNow, result); err != nil {
		t.Fatalf("applyDemotion() unexpected error: %v", err)
	}
	if len(repo.updatedRoles) != 0 {
		t.Errorf("roles updated = %v, want none", repo.updatedRoles)
	}
	if len(result.Demotions) != 0 || len(result.Promotions) != 0 || result.Cleared != 0 || result.Marked != 0 {
		t.Errorf("result = %+v, want it untouched", *result)
	}
}

// Each non-demotion outcome writes exactly its own field; a mix-up here would
// silently mis-report a run to the operator.
func TestRoleChecker_ApplyDemotion_OutcomeBookkeeping(t *testing.T) {
	tests := []struct {
		name        string
		outcome     demotionOutcome
		wantCleared int
		wantMarked  int
	}{
		{"demotionNone records nothing", demotionNone, 0, 0},
		{"demotionWait records nothing", demotionWait, 0, 0},
		{"demotionClear counts a clear", demotionClear, 1, 0},
		{"demotionMark counts a mark", demotionMark, 0, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockRoleCheckerRepo()
			rc := NewRoleChecker(repo, testLogger(), roleCheckClock)
			u := &domain.User{ID: "user-1", DisplayName: "Bob", TrustScore: 65, Role: domain.RoleMember}
			result := &RoleCheckResult{}

			if err := rc.applyDemotion(context.Background(), u, demotionDecision{Outcome: tt.outcome}, roleCheckNow, result); err != nil {
				t.Fatalf("applyDemotion() unexpected error: %v", err)
			}
			if result.Cleared != tt.wantCleared {
				t.Errorf("Cleared = %d, want %d", result.Cleared, tt.wantCleared)
			}
			if result.Marked != tt.wantMarked {
				t.Errorf("Marked = %d, want %d", result.Marked, tt.wantMarked)
			}
			if len(result.Demotions) != 0 {
				t.Errorf("Demotions = %d, want 0", len(result.Demotions))
			}
		})
	}
}

// The checker is a nightly sweep of the whole town, so one user's write
// failure must not abandon everyone after them.
func TestRoleChecker_Run_ContinuesAfterAPerUserFailure(t *testing.T) {
	thirtyOneDaysAgo := roleCheckNow.AddDate(0, 0, -31)
	repo := newMockRoleCheckerRepo()
	repo.clearTrustBelowErr = errors.New("db write failed")
	repo.users = []*domain.User{
		{
			ID: "recovered", DisplayName: "Dave", TrustScore: 90,
			Role: domain.RoleMember, JoinedAt: roleCheckNow.AddDate(0, 0, -100),
			TrustBelowSince: &thirtyOneDaysAgo,
		},
		{
			ID: "promotable", DisplayName: "Alice", TrustScore: 90,
			Role: domain.RoleMember, JoinedAt: roleCheckNow.AddDate(0, 0, -100),
		},
	}
	repo.modVouchCounts = map[string]int64{"promotable": 3}

	rc := NewRoleChecker(repo, testLogger(), roleCheckClock)
	result, err := rc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if result.Cleared != 0 {
		t.Errorf("Cleared = %d, want 0 after the write failed", result.Cleared)
	}
	if len(result.Promotions) != 1 {
		t.Fatalf("Promotions = %d, want the later user still promoted", len(result.Promotions))
	}
}

// A user whose demotion bookkeeping fails must not be reported as demoted,
// otherwise the run summary would claim a role change that never landed.
func TestRoleChecker_Run_FailedDemotionWriteIsNotReported(t *testing.T) {
	thirtyOneDaysAgo := roleCheckNow.AddDate(0, 0, -31)
	demotable := []*domain.User{
		{
			ID: "user-1", DisplayName: "Bob", TrustScore: 60,
			Role: domain.RoleMember, JoinedAt: roleCheckNow.AddDate(0, 0, -100),
			TrustBelowSince: &thirtyOneDaysAgo,
		},
	}

	tests := []struct {
		name  string
		setup func(*mockRoleCheckerRepo)
	}{
		{"role write fails", func(m *mockRoleCheckerRepo) { m.updateRoleErr = errors.New("db write failed") }},
		{"role history write fails", func(m *mockRoleCheckerRepo) { m.createRoleHistoryErr = errors.New("db write failed") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockRoleCheckerRepo()
			repo.users = demotable
			tt.setup(repo)

			result, err := NewRoleChecker(repo, testLogger(), roleCheckClock).Run(context.Background())
			if err != nil {
				t.Fatalf("Run() unexpected error: %v", err)
			}
			if len(result.Demotions) != 0 {
				t.Errorf("Demotions = %d, want 0 when the write failed", len(result.Demotions))
			}
		})
	}
}

// Clearing the timer is cleanup after the demotion has already committed, so
// its failure is logged rather than un-reporting the demotion.
func TestRoleChecker_Run_DemotionSurvivesAFailedTimerClear(t *testing.T) {
	thirtyOneDaysAgo := roleCheckNow.AddDate(0, 0, -31)
	repo := newMockRoleCheckerRepo()
	repo.clearTrustBelowErr = errors.New("db write failed")
	repo.users = []*domain.User{
		{
			ID: "user-1", DisplayName: "Bob", TrustScore: 60,
			Role: domain.RoleMember, JoinedAt: roleCheckNow.AddDate(0, 0, -100),
			TrustBelowSince: &thirtyOneDaysAgo,
		},
	}

	result, err := NewRoleChecker(repo, testLogger(), roleCheckClock).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if len(result.Demotions) != 1 {
		t.Fatalf("Demotions = %d, want 1", len(result.Demotions))
	}
	if repo.updatedRoles["user-1"] != domain.RolePending {
		t.Errorf("role = %q, want %q", repo.updatedRoles["user-1"], domain.RolePending)
	}
}

// Failing to count moderator vouches must not promote anyone by default, and
// must not fail the whole run.
func TestRoleChecker_Run_VouchCountFailureBlocksPromotion(t *testing.T) {
	repo := newMockRoleCheckerRepo()
	repo.countModVouchesErr = errors.New("graph unavailable")
	repo.users = []*domain.User{
		{
			ID: "user-1", DisplayName: "Alice", TrustScore: 90,
			Role: domain.RoleMember, JoinedAt: roleCheckNow.AddDate(0, 0, -100),
		},
	}

	result, err := NewRoleChecker(repo, testLogger(), roleCheckClock).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if len(result.Promotions) != 0 {
		t.Errorf("Promotions = %d, want 0 when the vouch count is unknown", len(result.Promotions))
	}
	if len(repo.updatedRoles) != 0 {
		t.Errorf("roles updated = %v, want none", repo.updatedRoles)
	}
}

// The timer writes are the only side effect of the non-demotion outcomes, so
// their failures have to surface rather than be counted as done.
func TestRoleChecker_ApplyDemotion_TimerWriteErrors(t *testing.T) {
	tests := []struct {
		name    string
		outcome demotionOutcome
		setup   func(*mockRoleCheckerRepo)
	}{
		{"clearing the timer fails", demotionClear, func(m *mockRoleCheckerRepo) {
			m.clearTrustBelowErr = errors.New("db write failed")
		}},
		{"setting the timer fails", demotionMark, func(m *mockRoleCheckerRepo) {
			m.updateTrustBelowErr = errors.New("db write failed")
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockRoleCheckerRepo()
			tt.setup(repo)
			rc := NewRoleChecker(repo, testLogger(), roleCheckClock)
			u := &domain.User{ID: "user-1", DisplayName: "Bob", TrustScore: 65, Role: domain.RoleMember}
			result := &RoleCheckResult{}

			err := rc.applyDemotion(context.Background(), u, demotionDecision{Outcome: tt.outcome}, roleCheckNow, result)
			if err == nil {
				t.Fatal("applyDemotion() expected an error, got nil")
			}
			if result.Cleared != 0 || result.Marked != 0 {
				t.Errorf("result = %+v, want nothing counted after a failed write", *result)
			}
		})
	}
}

func TestRoleChecker_NilClock(t *testing.T) {
	repo := newMockRoleCheckerRepo()
	repo.users = []*domain.User{}

	rc := NewRoleChecker(repo, testLogger(), nil)
	result, err := rc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if result.UsersChecked != 0 {
		t.Errorf("UsersChecked = %d, want 0", result.UsersChecked)
	}
}

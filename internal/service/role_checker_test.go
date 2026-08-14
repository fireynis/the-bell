package service

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
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
					// Members are judged against 35, not the moderator's 70.
					ID:          "freshly-dipped",
					DisplayName: "Erin",
					TrustScore:  30.0,
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
					TrustScore:      25.0,
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
			TrustScore:      25.0,
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
			ID: "user-1", DisplayName: "Bob", TrustScore: 25,
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
			ID: "user-1", DisplayName: "Bob", TrustScore: 25,
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

// --- Demotion runs on a freshly computed score ---

// recalcFixture is the history one user's trust recomputes from.
type recalcFixture struct {
	posts     int64
	reactions int64
	vouches   int64
	avgTrust  float64
}

// stubRecalcInputs is a TrustInputs over a fixed cast of users. It drives the
// real CalcCompositeTrust rather than handing back a stand-in number, so these
// tests fail if the refresh stops reflecting what the scoring model would say.
type stubRecalcInputs struct {
	users    map[string]*domain.User
	fixtures map[string]recalcFixture
	// errForUser fails the lookup for named users only.
	errForUser map[string]error
}

func (s *stubRecalcInputs) GetUserByID(_ context.Context, id string) (*domain.User, error) {
	if err, ok := s.errForUser[id]; ok {
		return nil, err
	}
	u, ok := s.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	return u, nil
}

func (s *stubRecalcInputs) CountPostsByAuthorSince(_ context.Context, id string, _ time.Time) (int64, error) {
	return s.fixtures[id].posts, nil
}

func (s *stubRecalcInputs) CountReactionsReceivedByAuthorSince(_ context.Context, id string, _ time.Time) (int64, error) {
	return s.fixtures[id].reactions, nil
}

func (s *stubRecalcInputs) CountActiveVouchesWithAvgTrust(_ context.Context, id string) (int64, float64, error) {
	f := s.fixtures[id]
	return f.vouches, f.avgTrust, nil
}

func (s *stubRecalcInputs) ListActivePenaltiesByUser(_ context.Context, _ string) ([]domain.TrustPenalty, error) {
	return nil, nil
}

// stubTrustScoreWriter records the scores the refresh persists.
type stubTrustScoreWriter struct {
	scores map[string]float64
	err    error
}

func newStubTrustScoreWriter() *stubTrustScoreWriter {
	return &stubTrustScoreWriter{scores: make(map[string]float64)}
}

func (s *stubTrustScoreWriter) UpdateUserTrustScore(_ context.Context, id string, score float64) error {
	if s.err != nil {
		return s.err
	}
	s.scores[id] = score
	return nil
}

// withRefresher wires a checker over a cast of users whose stored score is the
// column default of 50 — what every user in a town that has never recalculated
// anything is sitting at.
func withRefresher(t *testing.T, users []*domain.User, fixtures map[string]recalcFixture) (*RoleChecker, *mockRoleCheckerRepo, *stubTrustScoreWriter) {
	t.Helper()

	repo := newMockRoleCheckerRepo()
	repo.users = users

	byID := make(map[string]*domain.User, len(users))
	for _, u := range users {
		byID[u.ID] = u
	}

	writer := newStubTrustScoreWriter()
	rc := NewRoleChecker(repo, testLogger(), roleCheckClock)
	rc.SetTrustRefresher(&stubRecalcInputs{users: byID, fixtures: fixtures}, writer)
	return rc, repo, writer
}

// The bug this pins: users are created at trust 50.0 and, without Redis, no
// worker ever recomputes that number. The demotion threshold is a flat 70.0,
// so thirty days after a town opens check-roles demoted the entire membership
// to pending on the strength of a default nobody had ever measured.
//
// The fix is to measure it first. These three have two years of tenure, capped
// activity and seven vouches from perfect-trust neighbours — the composite says
// 100 — so the stale 50 is the only thing that was ever going to demote them.
func TestRoleChecker_Run_DoesNotDemoteOnANeverRecalculatedScore(t *testing.T) {
	thirtyOneDaysAgo := roleCheckNow.AddDate(0, 0, -31)
	users := []*domain.User{
		{ID: "user-1", DisplayName: "Alice", TrustScore: 50, Role: domain.RoleMember,
			JoinedAt: roleCheckNow.AddDate(-2, 0, 0), TrustBelowSince: &thirtyOneDaysAgo},
		{ID: "user-2", DisplayName: "Bob", TrustScore: 50, Role: domain.RoleMember,
			JoinedAt: roleCheckNow.AddDate(-2, 0, 0), TrustBelowSince: &thirtyOneDaysAgo},
		{ID: "user-3", DisplayName: "Carol", TrustScore: 50, Role: domain.RoleModerator,
			JoinedAt: roleCheckNow.AddDate(-2, 0, 0), TrustBelowSince: &thirtyOneDaysAgo},
	}
	established := recalcFixture{posts: 90, reactions: 270, vouches: 7, avgTrust: 100}
	fixtures := map[string]recalcFixture{"user-1": established, "user-2": established, "user-3": established}

	rc, repo, writer := withRefresher(t, users, fixtures)
	result, err := rc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if len(result.Demotions) != 0 {
		t.Errorf("Demotions = %d, want 0 — the whole town was demoted off a default nobody measured",
			len(result.Demotions))
	}
	if len(repo.updatedRoles) != 0 {
		t.Errorf("roles updated = %v, want none", repo.updatedRoles)
	}
	// Recovering above the threshold is what the fresh score reports, so the
	// timers come off too.
	if result.Cleared != 3 {
		t.Errorf("Cleared = %d, want 3", result.Cleared)
	}

	// The refreshed score is persisted, not just used and thrown away: without
	// Redis this run is the only thing that ever recomputes it, and the posting
	// and vouching gates read the stored value.
	for _, u := range users {
		if got := writer.scores[u.ID]; got != 100 {
			t.Errorf("persisted score for %q = %v, want 100", u.ID, got)
		}
		if u.TrustScore != 100 {
			t.Errorf("in-memory score for %q = %v, want 100", u.ID, u.TrustScore)
		}
	}
}

// The other half of the fix: refreshing must not amount to switching demotion
// off. This user's stored 50 and freshly computed 30 are both below the
// threshold, and the fresh one is what the reason string reports.
func TestRoleChecker_Run_StillDemotesOnAFreshlyLowScore(t *testing.T) {
	thirtyOneDaysAgo := roleCheckNow.AddDate(0, 0, -31)
	users := []*domain.User{
		{ID: "user-1", DisplayName: "Dan", TrustScore: 50, Role: domain.RoleMember,
			JoinedAt: roleCheckNow, TrustBelowSince: &thirtyOneDaysAgo},
	}

	rc, repo, writer := withRefresher(t, users, map[string]recalcFixture{})
	result, err := rc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if len(result.Demotions) != 1 {
		t.Fatalf("Demotions = %d, want 1", len(result.Demotions))
	}
	if repo.updatedRoles["user-1"] != domain.RolePending {
		t.Errorf("role = %q, want %q", repo.updatedRoles["user-1"], domain.RolePending)
	}
	if got := writer.scores["user-1"]; got != 30 {
		t.Errorf("persisted score = %v, want 30 (the clean moderation component alone)", got)
	}
	if want := "trust 30.0"; !strings.Contains(result.Demotions[0].Reason, want) {
		t.Errorf("reason = %q, want it to quote the refreshed score (%q)", result.Demotions[0].Reason, want)
	}
}

// A score that could not be computed is not evidence of anything. Skipping the
// user leaves their role alone until the next run, which is the safe direction:
// the alternative judges them on a number the run just failed to confirm.
func TestRoleChecker_Run_RefreshFailureSkipsTheUser(t *testing.T) {
	thirtyOneDaysAgo := roleCheckNow.AddDate(0, 0, -31)
	users := []*domain.User{
		{ID: "user-1", DisplayName: "Erin", TrustScore: 10, Role: domain.RoleMember,
			JoinedAt: roleCheckNow.AddDate(-2, 0, 0), TrustBelowSince: &thirtyOneDaysAgo},
	}

	rc, repo, _ := withRefresher(t, users, map[string]recalcFixture{})
	rc.inputs = &stubRecalcInputs{
		users:      map[string]*domain.User{"user-1": users[0]},
		errForUser: map[string]error{"user-1": errors.New("db connection lost")},
	}

	result, err := rc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if len(result.Demotions) != 0 {
		t.Errorf("Demotions = %d, want 0 when the score could not be recomputed", len(result.Demotions))
	}
	if len(repo.updatedRoles) != 0 {
		t.Errorf("roles updated = %v, want none", repo.updatedRoles)
	}
}

// A council member bricked by the old scoring bug is repaired by the same
// refresh. Without Redis this run is the only recalculation a town ever gets,
// so it has to reach council too, even though the policy never changes their
// role.
func TestRoleChecker_Run_RefreshesCouncilWithoutTouchingTheirRole(t *testing.T) {
	users := []*domain.User{
		{ID: "council-1", DisplayName: "Frank", TrustScore: 33, Role: domain.RoleCouncil,
			IsActive: true, JoinedAt: roleCheckNow.AddDate(0, 0, -10)},
	}

	rc, repo, writer := withRefresher(t, users, map[string]recalcFixture{})
	result, err := rc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if got := writer.scores["council-1"]; got != 100 {
		t.Errorf("persisted score = %v, want 100 — council was left below the vouching threshold", got)
	}
	if len(repo.updatedRoles) != 0 || len(result.Demotions) != 0 || len(result.Promotions) != 0 {
		t.Errorf("council was touched by the role policy: roles=%v demotions=%d promotions=%d",
			repo.updatedRoles, len(result.Demotions), len(result.Promotions))
	}
	if result.Marked != 0 || result.Cleared != 0 {
		t.Errorf("council timers were written: marked=%d cleared=%d", result.Marked, result.Cleared)
	}
}

// A checker built without a refresher keeps working on stored scores. The
// wiring in internal/app always supplies one; this only pins that the
// dependency is optional rather than a nil dereference waiting to happen.
func TestRoleChecker_Run_WithoutARefresherUsesStoredScores(t *testing.T) {
	thirtyOneDaysAgo := roleCheckNow.AddDate(0, 0, -31)
	repo := newMockRoleCheckerRepo()
	repo.users = []*domain.User{
		{ID: "user-1", DisplayName: "Bob", TrustScore: 25, Role: domain.RoleMember,
			JoinedAt: roleCheckNow.AddDate(0, 0, -100), TrustBelowSince: &thirtyOneDaysAgo},
	}

	result, err := NewRoleChecker(repo, testLogger(), roleCheckClock).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if len(result.Demotions) != 1 {
		t.Errorf("Demotions = %d, want 1", len(result.Demotions))
	}
}

// Persisting the refreshed score is best-effort: the run still has the right
// number in hand, so a failed write must not cost the user a role evaluation.
func TestRoleChecker_Run_ScoreWriteFailureStillEvaluates(t *testing.T) {
	thirtyOneDaysAgo := roleCheckNow.AddDate(0, 0, -31)
	users := []*domain.User{
		{ID: "user-1", DisplayName: "Gwen", TrustScore: 50, Role: domain.RoleMember,
			JoinedAt: roleCheckNow.AddDate(-2, 0, 0), TrustBelowSince: &thirtyOneDaysAgo},
	}
	fixtures := map[string]recalcFixture{
		"user-1": {posts: 90, reactions: 270, vouches: 7, avgTrust: 100},
	}

	rc, repo, writer := withRefresher(t, users, fixtures)
	writer.err = errors.New("db write failed")

	result, err := rc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if len(result.Demotions) != 0 {
		t.Errorf("Demotions = %d, want 0 — the computed score was healthy", len(result.Demotions))
	}
	if result.Cleared != 1 {
		t.Errorf("Cleared = %d, want 1", result.Cleared)
	}
	if len(repo.updatedRoles) != 0 {
		t.Errorf("roles updated = %v, want none", repo.updatedRoles)
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

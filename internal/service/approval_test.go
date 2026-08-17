package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// mockApprovalUserRepo implements ApprovalUserRepository for testing.
var approvalFixedNow = time.Date(2026, 3, 1, 14, 0, 0, 0, time.UTC)

// --- ListPending ---

func TestApprovalService_ListPending_Success(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "true"
	userRepo.users["pending-1"] = &domain.User{
		ID: "pending-1", Role: domain.RolePending, IsActive: true,
		CreatedAt: approvalFixedNow,
	}
	userRepo.users["member-1"] = &domain.User{
		ID: "member-1", Role: domain.RoleMember, IsActive: true,
	}

	svc := NewApprovalService(userRepo, configRepo)
	users, total, err := svc.ListPending(context.Background(), "", 25, 0)
	if err != nil {
		t.Fatalf("ListPending() unexpected error: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("ListPending() returned %d users, want 1", len(users))
	}
	if users[0].ID != "pending-1" {
		t.Errorf("ListPending()[0].ID = %q, want %q", users[0].ID, "pending-1")
	}
	if total != 1 {
		t.Errorf("ListPending() total = %d, want 1", total)
	}
}

func TestApprovalService_ListPending_NotBootstrapMode(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "false"

	svc := NewApprovalService(userRepo, configRepo)
	_, _, err := svc.ListPending(context.Background(), "", 25, 0)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("ListPending() error = %v, want %v", err, ErrForbidden)
	}
}

func TestApprovalService_ListPending_NoBootstrapKey(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo() // empty config, key not found

	svc := NewApprovalService(userRepo, configRepo)
	_, _, err := svc.ListPending(context.Background(), "", 25, 0)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("ListPending() error = %v, want %v", err, ErrForbidden)
	}
}

// --- ListPending: paging and search ---

// bootstrapQueue puts the town in bootstrap mode with the named applicants
// waiting, joined a day apart so the oldest-first ordering is unambiguous.
func bootstrapQueue(t *testing.T, names ...string) (*fakeUserStore, *ApprovalService) {
	t.Helper()

	store := newFakeUserStore()
	for i, name := range names {
		store.add(&domain.User{
			ID:          fmt.Sprintf("pending-%d", i),
			DisplayName: name,
			Role:        domain.RolePending,
			IsActive:    true,
			// Earlier in the list means earlier in town: index 0 waited longest.
			JoinedAt: approvalFixedNow.AddDate(0, 0, i),
		})
	}

	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "true"
	return store, NewApprovalService(store, configRepo)
}

// The queue is FIFO: the applicant who has waited longest is reviewed first, so
// a flood of newer registrations cannot bury somebody who signed up last week.
func TestApprovalService_ListPending_OldestFirst(t *testing.T) {
	_, svc := bootstrapQueue(t, "First", "Second", "Third")

	users, _, err := svc.ListPending(context.Background(), "", 25, 0)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}

	var got []string
	for _, u := range users {
		got = append(got, u.DisplayName)
	}
	want := []string{"First", "Second", "Third"}
	if len(got) != len(want) {
		t.Fatalf("queue = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("queue = %v, want %v", got, want)
		}
	}
}

// The same bounds as the directory, and for the same reason: a caller asking
// for too much gets the ceiling rather than an error.
func TestApprovalService_ListPending_BoundsTheLimit(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{"zero means the default", 0, DirectoryDefaultLimit},
		{"negative means the default", -5, DirectoryDefaultLimit},
		{"a value in range is passed through", 10, 10},
		{"the ceiling is allowed", DirectoryMaxLimit, DirectoryMaxLimit},
		{"above the ceiling is clamped, not rejected", 5000, DirectoryMaxLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, svc := bootstrapQueue(t)

			if _, _, err := svc.ListPending(context.Background(), "", tt.limit, 0); err != nil {
				t.Fatalf("ListPending: %v", err)
			}
			if store.pendingLimit != tt.want {
				t.Errorf("repository asked for limit %d, want %d", store.pendingLimit, tt.want)
			}
		})
	}
}

func TestApprovalService_ListPending_RejectsANegativeOffset(t *testing.T) {
	_, svc := bootstrapQueue(t)

	_, _, err := svc.ListPending(context.Background(), "", 25, -1)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want %v", err, ErrValidation)
	}
}

func TestApprovalService_ListPending_RejectsAnOverlongQuery(t *testing.T) {
	_, svc := bootstrapQueue(t)

	_, _, err := svc.ListPending(context.Background(), strings.Repeat("a", maxDirectorySearchLength+1), 25, 0)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want %v", err, ErrValidation)
	}

	if _, _, err := svc.ListPending(context.Background(), strings.Repeat("a", maxDirectorySearchLength), 25, 0); err != nil {
		t.Errorf("a query at the limit was rejected: %v", err)
	}
}

// Whitespace around a search term is a typing artefact, not part of the name
// being looked for.
func TestApprovalService_ListPending_TrimsTheQuery(t *testing.T) {
	store, svc := bootstrapQueue(t, "Ada")

	if _, _, err := svc.ListPending(context.Background(), "  Ada  ", 25, 0); err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if store.pendingQuery != "Ada" {
		t.Errorf("repository asked for %q, want %q", store.pendingQuery, "Ada")
	}
}

// The total sizes the whole match rather than the page, which is what lets the
// council's screen say how many neighbours are waiting from the first request.
func TestApprovalService_ListPending_TotalCountsEveryMatch(t *testing.T) {
	_, svc := bootstrapQueue(t, "Ada", "Bo", "Cai", "Dev", "Eun")

	users, total, err := svc.ListPending(context.Background(), "", 2, 0)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("page holds %d users, want 2", len(users))
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
}

// A filtered queue reports the size of the filter, not of the queue: a council
// member searching a name should not be told 50 people match when one does.
func TestApprovalService_ListPending_TotalFollowsTheSearch(t *testing.T) {
	_, svc := bootstrapQueue(t, "Ada Lovelace", "Bo Zhang", "Adam Smith")

	users, total, err := svc.ListPending(context.Background(), "ada", 25, 0)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(users) != 2 || total != 2 {
		t.Errorf("got %d users and total %d, want 2 and 2", len(users), total)
	}
}

func TestApprovalService_ListPending_ReportsRepositoryFailures(t *testing.T) {
	t.Run("the page", func(t *testing.T) {
		store, svc := bootstrapQueue(t)
		store.pendingPageErr = errors.New("db connection lost")

		if _, _, err := svc.ListPending(context.Background(), "", 25, 0); err == nil {
			t.Fatal("expected an error when the listing fails")
		}
	})

	t.Run("the count", func(t *testing.T) {
		store, svc := bootstrapQueue(t)
		store.countPendingErr = errors.New("db connection lost")

		if _, _, err := svc.ListPending(context.Background(), "", 25, 0); err == nil {
			t.Fatal("expected an error when the count fails")
		}
	})
}

// --- Approve ---

func TestApprovalService_Approve_Success(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "true"
	userRepo.users["pending-1"] = &domain.User{
		ID: "pending-1", Role: domain.RolePending, IsActive: true,
	}

	svc := NewApprovalService(userRepo, configRepo)
	user, err := svc.Approve(context.Background(), "pending-1")
	if err != nil {
		t.Fatalf("Approve() unexpected error: %v", err)
	}
	if user.Role != domain.RoleMember {
		t.Errorf("user.Role = %q, want %q", user.Role, domain.RoleMember)
	}
}

func TestApprovalService_Approve_NotBootstrapMode(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "false"

	svc := NewApprovalService(userRepo, configRepo)
	_, err := svc.Approve(context.Background(), "pending-1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Approve() error = %v, want %v", err, ErrForbidden)
	}
}

func TestApprovalService_Approve_UserNotFound(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "true"

	svc := NewApprovalService(userRepo, configRepo)
	_, err := svc.Approve(context.Background(), "nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Approve() error = %v, want %v", err, ErrNotFound)
	}
}

func TestApprovalService_Approve_NotPending(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "true"
	userRepo.users["member-1"] = &domain.User{
		ID: "member-1", Role: domain.RoleMember, IsActive: true,
	}

	svc := NewApprovalService(userRepo, configRepo)
	_, err := svc.Approve(context.Background(), "member-1")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Approve() error = %v, want %v", err, ErrValidation)
	}
}

func TestApprovalService_Approve_InactiveUser(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "true"
	userRepo.users["inactive-1"] = &domain.User{
		ID: "inactive-1", Role: domain.RolePending, IsActive: false,
	}

	svc := NewApprovalService(userRepo, configRepo)
	_, err := svc.Approve(context.Background(), "inactive-1")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Approve() error = %v, want %v", err, ErrValidation)
	}
}

func TestApprovalService_Approve_ExitsBootstrapAt20Members(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "true"

	// Add 19 existing active members
	for i := 0; i < 19; i++ {
		id := fmt.Sprintf("member-%d", i)
		userRepo.users[id] = &domain.User{
			ID: id, Role: domain.RoleMember, IsActive: true,
		}
	}
	// Add the pending user who will become #20
	userRepo.users["pending-1"] = &domain.User{
		ID: "pending-1", Role: domain.RolePending, IsActive: true,
	}

	svc := NewApprovalService(userRepo, configRepo)
	_, err := svc.Approve(context.Background(), "pending-1")
	if err != nil {
		t.Fatalf("Approve() unexpected error: %v", err)
	}

	if configRepo.config["bootstrap_mode"] != "false" {
		t.Errorf("bootstrap_mode = %q, want %q", configRepo.config["bootstrap_mode"], "false")
	}
}

func TestApprovalService_Approve_StaysBootstrapBelow20(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "true"

	// Add 5 existing active members
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("member-%d", i)
		userRepo.users[id] = &domain.User{
			ID: id, Role: domain.RoleMember, IsActive: true,
		}
	}
	userRepo.users["pending-1"] = &domain.User{
		ID: "pending-1", Role: domain.RolePending, IsActive: true,
	}

	svc := NewApprovalService(userRepo, configRepo)
	_, err := svc.Approve(context.Background(), "pending-1")
	if err != nil {
		t.Fatalf("Approve() unexpected error: %v", err)
	}

	if configRepo.config["bootstrap_mode"] != "true" {
		t.Errorf("bootstrap_mode = %q, want %q (should remain true below threshold)", configRepo.config["bootstrap_mode"], "true")
	}
}

// Leaving bootstrap mode is a one-way transition, so a failure to evaluate it
// must reach the caller. Swallowing this error would leave the town in
// bootstrap mode with nothing recorded anywhere.
func TestApprovalService_Approve_CountErrorIsReported(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	userRepo.countErr = errors.New("db connection lost")
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "true"
	userRepo.users["pending-1"] = &domain.User{
		ID: "pending-1", Role: domain.RolePending, IsActive: true,
	}

	svc := NewApprovalService(userRepo, configRepo)
	user, err := svc.Approve(context.Background(), "pending-1")
	if err == nil {
		t.Fatal("Approve() expected an error, got nil")
	}
	if !errors.Is(err, userRepo.countErr) {
		t.Errorf("error = %v, want it to wrap %v", err, userRepo.countErr)
	}
	if user != nil {
		t.Errorf("Approve() = %+v, want nil alongside the error", user)
	}
	// The role change committed before the count ran; the error says the
	// bootstrap-exit check did not happen, not that the approval was undone.
	if userRepo.users["pending-1"].Role != domain.RoleMember {
		t.Errorf("stored role = %q, want the approval to have stuck at %q",
			userRepo.users["pending-1"].Role, domain.RoleMember)
	}
}

// Failing to write bootstrap_mode=false is the failure that matters most: the
// town would otherwise stay in bootstrap mode forever with no trace.
func TestApprovalService_Approve_BootstrapExitWriteErrorIsReported(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "true"

	for i := 0; i < bootstrapExitThreshold-1; i++ {
		id := fmt.Sprintf("member-%d", i)
		userRepo.users[id] = &domain.User{ID: id, Role: domain.RoleMember, IsActive: true}
	}
	userRepo.users["pending-1"] = &domain.User{
		ID: "pending-1", Role: domain.RolePending, IsActive: true,
	}
	configRepo.setErr = errors.New("db write failed")

	svc := NewApprovalService(userRepo, configRepo)
	user, err := svc.Approve(context.Background(), "pending-1")
	if err == nil {
		t.Fatal("Approve() expected an error, got nil")
	}
	if !errors.Is(err, configRepo.setErr) {
		t.Errorf("error = %v, want it to wrap %v", err, configRepo.setErr)
	}
	if user != nil {
		t.Errorf("Approve() = %+v, want nil alongside the error", user)
	}
	if configRepo.config["bootstrap_mode"] != "true" {
		t.Errorf("bootstrap_mode = %q, want it unchanged at %q after the failed write",
			configRepo.config["bootstrap_mode"], "true")
	}
}

// Below the threshold the config is never written, so a broken config store
// cannot fail an ordinary approval.
func TestApprovalService_Approve_BelowThresholdDoesNotTouchConfig(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "true"
	userRepo.users["pending-1"] = &domain.User{
		ID: "pending-1", Role: domain.RolePending, IsActive: true,
	}
	configRepo.setErr = errors.New("db write failed")

	svc := NewApprovalService(userRepo, configRepo)
	if _, err := svc.Approve(context.Background(), "pending-1"); err != nil {
		t.Fatalf("Approve() unexpected error: %v", err)
	}
}

// --- Self-healing bootstrap exit ---

// stuckInBootstrap builds the state a failed exit check leaves behind:
// bootstrap_mode still true with the town already over the threshold. It is
// reachable in production because Approve evaluates the exit AFTER committing
// the role change, and re-approving that user fails the pending guard — so
// without a second look, nothing ever re-evaluates it.
func stuckInBootstrap(t *testing.T, activeMembers int) (*fakeUserStore, *mockConfigRepo, *ApprovalService) {
	t.Helper()

	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "true"
	for i := range activeMembers {
		id := fmt.Sprintf("member-%d", i)
		userRepo.users[id] = &domain.User{ID: id, Role: domain.RoleMember, IsActive: true}
	}
	userRepo.users["pending-1"] = &domain.User{
		ID: "pending-1", Role: domain.RolePending, IsActive: true,
	}

	svc := NewApprovalService(userRepo, configRepo)
	svc.logger = discardLogger()
	return userRepo, configRepo, svc
}

// Any call down the approval path repairs the flag, so the entry point rather
// than one method is what carries the check.
func TestApprovalService_ReEvaluatesAMissedBootstrapExit(t *testing.T) {
	tests := []struct {
		name string
		call func(*ApprovalService) error
	}{
		{"approve", func(s *ApprovalService) error {
			_, err := s.Approve(context.Background(), "pending-1")
			return err
		}},
		{"list pending", func(s *ApprovalService) error {
			_, _, err := s.ListPending(context.Background(), "", 25, 0)
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userRepo, configRepo, svc := stuckInBootstrap(t, bootstrapExitThreshold)

			// The town has left bootstrap, so the call is refused for the same
			// reason it would be if the flip had happened on time.
			if err := tt.call(svc); !errors.Is(err, ErrForbidden) {
				t.Fatalf("error = %v, want %v", err, ErrForbidden)
			}
			if configRepo.config["bootstrap_mode"] != "false" {
				t.Errorf("bootstrap_mode = %q, want %q — the town is stuck in bootstrap mode forever",
					configRepo.config["bootstrap_mode"], "false")
			}
			// And the refusal is a refusal: nothing was approved on the way.
			if got := userRepo.users["pending-1"].Role; got != domain.RolePending {
				t.Errorf("pending user's role = %q, want it left at %q", got, domain.RolePending)
			}
		})
	}
}

// The repair must not fire early. A town below the threshold is doing exactly
// what bootstrap mode is for, and flipping the flag would strand its remaining
// pending residents with no way in.
func TestApprovalService_BelowThresholdIsNotFlippedByTheRecheck(t *testing.T) {
	userRepo, configRepo, svc := stuckInBootstrap(t, bootstrapExitThreshold-2)

	user, err := svc.Approve(context.Background(), "pending-1")
	if err != nil {
		t.Fatalf("Approve() unexpected error: %v", err)
	}
	if user.Role != domain.RoleMember {
		t.Errorf("role = %q, want %q", user.Role, domain.RoleMember)
	}
	if configRepo.config["bootstrap_mode"] != "true" {
		t.Errorf("bootstrap_mode = %q, want it left at %q", configRepo.config["bootstrap_mode"], "true")
	}
	// The approval took the count to one short of the threshold, so the town
	// stays in bootstrap for one more resident.
	count, err := userRepo.CountActiveMembers(context.Background())
	if err != nil {
		t.Fatalf("CountActiveMembers() unexpected error: %v", err)
	}
	if count != bootstrapExitThreshold-1 {
		t.Errorf("active members = %d, want %d", count, bootstrapExitThreshold-1)
	}
}

// The re-check runs on calls that are not about the exit at all, so it must not
// be able to fail one. A town that cannot be counted still approves its next
// resident — and Approve's own post-commit check, one step later, is what tells
// the caller the count failed.
func TestApprovalService_RecheckFailureDoesNotBlockTheApprovalPath(t *testing.T) {
	userRepo, configRepo, svc := stuckInBootstrap(t, bootstrapExitThreshold)
	configRepo.setErr = errors.New("db write failed")

	// The write fails, so the flag stays wrong — but ListPending still answers
	// rather than reporting the repair's failure as the caller's problem.
	pending, _, err := svc.ListPending(context.Background(), "", 25, 0)
	if err != nil {
		t.Fatalf("ListPending() error = %v; a failed repair must not fail the call", err)
	}
	if len(pending) != 1 {
		t.Errorf("%d pending users, want 1", len(pending))
	}
	if configRepo.config["bootstrap_mode"] != "true" {
		t.Errorf("bootstrap_mode = %q, want it unchanged at %q after the failed write",
			configRepo.config["bootstrap_mode"], "true")
	}

	// Same for a count that cannot be read.
	configRepo.setErr = nil
	userRepo.countErr = errors.New("db connection lost")
	if _, _, err := svc.ListPending(context.Background(), "", 25, 0); err != nil {
		t.Fatalf("ListPending() error = %v; an unreadable count must not fail the call", err)
	}
}

func TestApprovalService_Approve_RoleUpdateError(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	userRepo.updateRoleErr = errors.New("db write failed")
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "true"
	userRepo.users["pending-1"] = &domain.User{
		ID: "pending-1", Role: domain.RolePending, IsActive: true,
	}

	svc := NewApprovalService(userRepo, configRepo)
	_, err := svc.Approve(context.Background(), "pending-1")
	if err == nil {
		t.Fatal("Approve() expected error, got nil")
	}
}

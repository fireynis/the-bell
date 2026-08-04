package service

import (
	"context"
	"errors"
	"fmt"
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
	users, err := svc.ListPending(context.Background())
	if err != nil {
		t.Fatalf("ListPending() unexpected error: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("ListPending() returned %d users, want 1", len(users))
	}
	if users[0].ID != "pending-1" {
		t.Errorf("ListPending()[0].ID = %q, want %q", users[0].ID, "pending-1")
	}
}

func TestApprovalService_ListPending_NotBootstrapMode(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo()
	configRepo.config["bootstrap_mode"] = "false"

	svc := NewApprovalService(userRepo, configRepo)
	_, err := svc.ListPending(context.Background())
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("ListPending() error = %v, want %v", err, ErrForbidden)
	}
}

func TestApprovalService_ListPending_NoBootstrapKey(t *testing.T) {
	userRepo := newMockApprovalUserRepo()
	configRepo := newMockConfigRepo() // empty config, key not found

	svc := NewApprovalService(userRepo, configRepo)
	_, err := svc.ListPending(context.Background())
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("ListPending() error = %v, want %v", err, ErrForbidden)
	}
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

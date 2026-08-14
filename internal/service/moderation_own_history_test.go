package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// The member-facing history has one property no amount of correct output can
// substitute for: what it must NOT contain. These pin the three policy lines
// documented at the top of moderation_own_history.go — the member sees the
// decision and the reason, never the moderator, and never a penalty that is not
// their own.

const (
	ownTestMemberID    = "user-1"
	ownTestModeratorID = "mod-1"
	ownTestVoucherID   = "voucher-1"
)

var ownTestNow = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

// recordingActionLister answers with fixed actions and remembers the pagination
// it was asked for, which is the only way to tell a limit that was honoured
// from one that was dropped on the floor.
type recordingActionLister struct {
	actions    []*domain.ModerationAction
	err        error
	gotUserID  string
	gotLimit   int
	gotOffset  int
	byModCalls int
}

func (m *recordingActionLister) ListActionsByTarget(_ context.Context, targetUserID string, limit, offset int) ([]*domain.ModerationAction, error) {
	m.gotUserID, m.gotLimit, m.gotOffset = targetUserID, limit, offset
	return m.actions, m.err
}

func (m *recordingActionLister) ListActionsByModerator(_ context.Context, _ string, _, _ int) ([]*domain.ModerationAction, error) {
	m.byModCalls++
	return nil, nil
}

// warnAgainstMember is the action every test here starts from: a real decision,
// with a real reason, taken by somebody the member must not learn the name of.
func warnAgainstMember() *domain.ModerationAction {
	return &domain.ModerationAction{
		ID:                   "act-1",
		TargetUserID:         ownTestMemberID,
		ModeratorID:          ownTestModeratorID,
		ModeratorDisplayName: "Mallory",
		TargetDisplayName:    "Ada",
		Action:               domain.ActionWarn,
		Severity:             1,
		Reason:               "posting the same thing repeatedly",
		CreatedAt:            ownTestNow,
	}
}

func TestOwnHistory_ShowsTheDecisionAndTheReason(t *testing.T) {
	actions := &recordingActionLister{actions: []*domain.ModerationAction{warnAgainstMember()}}
	decays := ownTestNow.AddDate(0, 0, 90)
	penalties := newMockPenaltyListerS()
	penalties.penalties["act-1"] = []domain.TrustPenalty{
		{ID: "pen-1", UserID: ownTestMemberID, ModerationActionID: "act-1", PenaltyAmount: 5, HopDepth: 0, CreatedAt: ownTestNow, DecaysAt: &decays},
	}

	entries, err := NewModerationHistoryService(actions, penalties).
		OwnHistory(context.Background(), ownTestMemberID, 20, 0)
	if err != nil {
		t.Fatalf("OwnHistory() unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d entries, want 1", len(entries))
	}

	got := entries[0]
	if got.ID != "act-1" {
		t.Errorf("ID = %q, want act-1", got.ID)
	}
	if got.Action != domain.ActionWarn {
		t.Errorf("Action = %q, want warn", got.Action)
	}
	if got.Severity != 1 {
		t.Errorf("Severity = %d, want 1", got.Severity)
	}
	if got.Reason != "posting the same thing repeatedly" {
		t.Errorf("Reason = %q, want the moderator's words verbatim", got.Reason)
	}
	if !got.CreatedAt.Equal(ownTestNow) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, ownTestNow)
	}
	if got.Penalty == nil {
		t.Fatal("Penalty = nil, want the member's own 5-point penalty")
	}
	if got.Penalty.Amount != 5 {
		t.Errorf("Penalty.Amount = %v, want 5", got.Penalty.Amount)
	}
	if got.Penalty.DecaysAt == nil || !got.Penalty.DecaysAt.Equal(decays) {
		t.Errorf("Penalty.DecaysAt = %v, want %v", got.Penalty.DecaysAt, decays)
	}
}

// The load-bearing test. A response that carries the acting moderator's id or
// name anywhere in it — in any field, at any depth — is a failure, so this
// asserts over the serialized bytes rather than over the fields it remembers to
// check.
func TestOwnHistory_NamesNoModerator(t *testing.T) {
	actions := &recordingActionLister{actions: []*domain.ModerationAction{warnAgainstMember()}}
	penalties := newMockPenaltyListerS()
	penalties.penalties["act-1"] = []domain.TrustPenalty{
		{ID: "pen-1", UserID: ownTestMemberID, ModerationActionID: "act-1", PenaltyAmount: 5, HopDepth: 0, CreatedAt: ownTestNow},
	}

	entries, err := NewModerationHistoryService(actions, penalties).
		OwnHistory(context.Background(), ownTestMemberID, 20, 0)
	if err != nil {
		t.Fatalf("OwnHistory() unexpected error: %v", err)
	}

	encoded, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshalling entries: %v", err)
	}
	for _, forbidden := range []string{ownTestModeratorID, "Mallory", "moderator"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("member-facing history contains %q: %s", forbidden, encoded)
		}
	}
}

// The member is told what the action cost THEM. What it cost the people who
// vouched for them is those people's business, and showing it would tell the
// member exactly who is one hop away from them in the vouch graph.
func TestOwnHistory_ExcludesPenaltiesPropagatedToOthers(t *testing.T) {
	actions := &recordingActionLister{actions: []*domain.ModerationAction{warnAgainstMember()}}
	penalties := newMockPenaltyListerS()
	penalties.penalties["act-1"] = []domain.TrustPenalty{
		{ID: "pen-1", UserID: ownTestMemberID, ModerationActionID: "act-1", PenaltyAmount: 5, HopDepth: 0, CreatedAt: ownTestNow},
		{ID: "pen-2", UserID: ownTestVoucherID, ModerationActionID: "act-1", PenaltyAmount: 2.5, HopDepth: 1, CreatedAt: ownTestNow},
	}

	entries, err := NewModerationHistoryService(actions, penalties).
		OwnHistory(context.Background(), ownTestMemberID, 20, 0)
	if err != nil {
		t.Fatalf("OwnHistory() unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d entries, want 1", len(entries))
	}
	if entries[0].Penalty == nil || entries[0].Penalty.Amount != 5 {
		t.Fatalf("Penalty = %+v, want the member's own 5 points", entries[0].Penalty)
	}

	encoded, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshalling entries: %v", err)
	}
	if strings.Contains(string(encoded), ownTestVoucherID) {
		t.Errorf("member-facing history names a voucher: %s", encoded)
	}
	if strings.Contains(string(encoded), "2.5") {
		t.Errorf("member-facing history carries a propagated penalty amount: %s", encoded)
	}
}

// A penalty this action caused, at depth 0, but recorded against somebody else.
// Nothing produces this today; the check exists so that if something ever does,
// it does not become a window onto another member's moderation.
func TestOwnHistory_ExcludesADepthZeroPenaltyBelongingToSomebodyElse(t *testing.T) {
	actions := &recordingActionLister{actions: []*domain.ModerationAction{warnAgainstMember()}}
	penalties := newMockPenaltyListerS()
	penalties.penalties["act-1"] = []domain.TrustPenalty{
		{ID: "pen-1", UserID: "someone-else", ModerationActionID: "act-1", PenaltyAmount: 40, HopDepth: 0, CreatedAt: ownTestNow},
	}

	entries, err := NewModerationHistoryService(actions, penalties).
		OwnHistory(context.Background(), ownTestMemberID, 20, 0)
	if err != nil {
		t.Fatalf("OwnHistory() unexpected error: %v", err)
	}
	if entries[0].Penalty != nil {
		t.Errorf("Penalty = %+v, want nil for a penalty that is not the member's", entries[0].Penalty)
	}
}

// A ban's penalty never decays, and the member has to be told that rather than
// left to read a missing date as "unknown". The whole OwnPenalty being present
// with no DecaysAt is what says "permanent"; a nil OwnPenalty says "none".
func TestOwnHistory_DistinguishesAPermanentPenaltyFromNoPenalty(t *testing.T) {
	banned := warnAgainstMember()
	banned.ID = "act-ban"
	banned.Action = domain.ActionBan
	banned.Severity = 5
	banned.Reason = "repeated harassment after two warnings"

	unpropagated := warnAgainstMember()
	unpropagated.ID = "act-none"

	actions := &recordingActionLister{actions: []*domain.ModerationAction{banned, unpropagated}}
	penalties := newMockPenaltyListerS()
	penalties.penalties["act-ban"] = []domain.TrustPenalty{
		{ID: "pen-1", UserID: ownTestMemberID, ModerationActionID: "act-ban", PenaltyAmount: 100, HopDepth: 0, CreatedAt: ownTestNow, DecaysAt: nil},
	}

	entries, err := NewModerationHistoryService(actions, penalties).
		OwnHistory(context.Background(), ownTestMemberID, 20, 0)
	if err != nil {
		t.Fatalf("OwnHistory() unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("%d entries, want 2", len(entries))
	}

	if entries[0].Penalty == nil {
		t.Fatal("banned entry Penalty = nil, want a permanent 100-point penalty")
	}
	if entries[0].Penalty.Amount != 100 || entries[0].Penalty.DecaysAt != nil {
		t.Errorf("banned entry Penalty = %+v, want 100 points that never decay", entries[0].Penalty)
	}
	if entries[1].Penalty != nil {
		t.Errorf("unpropagated entry Penalty = %+v, want nil", entries[1].Penalty)
	}
}

// A mute's expiry is the answer to "when can I post again", so it has to
// survive the stripping.
func TestOwnHistory_CarriesTheRestrictionExpiry(t *testing.T) {
	expires := ownTestNow.Add(72 * time.Hour)
	muted := warnAgainstMember()
	muted.Action = domain.ActionMute
	muted.Severity = 3
	muted.ExpiresAt = &expires

	actions := &recordingActionLister{actions: []*domain.ModerationAction{muted}}

	entries, err := NewModerationHistoryService(actions, newMockPenaltyListerS()).
		OwnHistory(context.Background(), ownTestMemberID, 20, 0)
	if err != nil {
		t.Fatalf("OwnHistory() unexpected error: %v", err)
	}
	if entries[0].ExpiresAt == nil || !entries[0].ExpiresAt.Equal(expires) {
		t.Errorf("ExpiresAt = %v, want %v", entries[0].ExpiresAt, expires)
	}

	// Copied rather than aliased: mutating the result must not reach back into
	// the row the repository handed us.
	*entries[0].ExpiresAt = ownTestNow
	if !muted.ExpiresAt.Equal(expires) {
		t.Errorf("the source action's ExpiresAt changed to %v; the entry aliases it", muted.ExpiresAt)
	}
}

// Offset pagination, matching the moderator listing. A limit that never reaches
// the repository looks identical on a short history and fails on a long one.
func TestOwnHistory_ForwardsPaginationAndAsksOnlyForActionsAgainstTheMember(t *testing.T) {
	actions := &recordingActionLister{}

	if _, err := NewModerationHistoryService(actions, newMockPenaltyListerS()).
		OwnHistory(context.Background(), ownTestMemberID, 5, 10); err != nil {
		t.Fatalf("OwnHistory() unexpected error: %v", err)
	}

	if actions.gotUserID != ownTestMemberID {
		t.Errorf("asked about %q, want %q", actions.gotUserID, ownTestMemberID)
	}
	if actions.gotLimit != 5 || actions.gotOffset != 10 {
		t.Errorf("limit/offset = %d/%d, want 5/10", actions.gotLimit, actions.gotOffset)
	}
	// "Actions I took" is a council audit view of a moderator. This method must
	// never reach for it, whoever is asking.
	if actions.byModCalls != 0 {
		t.Errorf("ListActionsByModerator called %d times, want 0", actions.byModCalls)
	}
}

// A clean record is an empty list, not a nil one: the handler serializes this
// straight through and `null` where `[]` belongs is a client bug waiting to
// happen.
func TestOwnHistory_EmptyRecordIsAnEmptySlice(t *testing.T) {
	entries, err := NewModerationHistoryService(&recordingActionLister{}, newMockPenaltyListerS()).
		OwnHistory(context.Background(), ownTestMemberID, 20, 0)
	if err != nil {
		t.Fatalf("OwnHistory() unexpected error: %v", err)
	}
	if entries == nil {
		t.Fatal("entries = nil, want an empty slice")
	}
	if len(entries) != 0 {
		t.Errorf("%d entries, want 0", len(entries))
	}
}

func TestOwnHistory_EmptyUserIDIsRefused(t *testing.T) {
	_, err := NewModerationHistoryService(&recordingActionLister{}, newMockPenaltyListerS()).
		OwnHistory(context.Background(), "", 20, 0)
	if !errors.Is(err, ErrValidation) {
		t.Errorf("err = %v, want ErrValidation", err)
	}
}

func TestOwnHistory_RepositoryFailureIsReported(t *testing.T) {
	actions := &recordingActionLister{err: errors.New("db down")}

	_, err := NewModerationHistoryService(actions, newMockPenaltyListerS()).
		OwnHistory(context.Background(), ownTestMemberID, 20, 0)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

// The shim is what the member-facing route actually calls, so it has to reach
// the same stripped view rather than a second path into the audit trail.
func TestModerationActionService_OwnModerationHistory_DelegatesAndStrips(t *testing.T) {
	actions := newMockActionHistoryRepo()
	actions.actionsByTarget = []*domain.ModerationAction{warnAgainstMember()}
	penalties := newMockPenaltyListerS()
	penalties.penalties["act-1"] = []domain.TrustPenalty{
		{ID: "pen-1", UserID: ownTestMemberID, ModerationActionID: "act-1", PenaltyAmount: 5, HopDepth: 0, CreatedAt: ownTestNow},
		{ID: "pen-2", UserID: ownTestVoucherID, ModerationActionID: "act-1", PenaltyAmount: 2.5, HopDepth: 1, CreatedAt: ownTestNow},
	}

	svc := NewModerationActionService(actions, newMockActionUserLookup(), nil, nil, penalties, nil, fixedClock)

	entries, err := svc.OwnModerationHistory(context.Background(), ownTestMemberID, 20, 0)
	if err != nil {
		t.Fatalf("OwnModerationHistory() unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d entries, want 1", len(entries))
	}
	if entries[0].Penalty == nil || entries[0].Penalty.Amount != 5 {
		t.Errorf("Penalty = %+v, want the member's own 5 points", entries[0].Penalty)
	}

	encoded, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshalling entries: %v", err)
	}
	if strings.Contains(string(encoded), ownTestModeratorID) || strings.Contains(string(encoded), ownTestVoucherID) {
		t.Errorf("history reached the member unstripped: %s", encoded)
	}
}

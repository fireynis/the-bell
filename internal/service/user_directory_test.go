package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// directoryStore builds a store holding the users named, joined one day apart
// so the newest-first ordering is unambiguous.
func directoryStore(users ...*domain.User) *fakeUserStore {
	store := newFakeUserStore()
	for _, u := range users {
		store.add(u)
	}
	return store
}

func daysAgoAt(days int) time.Time {
	return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC).AddDate(0, 0, -days)
}

func directoryUser(id, name string, role domain.Role, joinedDaysAgo int) *domain.User {
	return &domain.User{
		ID:          id,
		DisplayName: name,
		Role:        role,
		IsActive:    true,
		JoinedAt:    daysAgoAt(joinedDaysAgo),
	}
}

// A limit outside the contract is bounded rather than refused, matching the
// feed: a caller asking for too much gets the ceiling, and one asking for
// nothing gets the default.
func TestUserService_ListDirectory_BoundsTheLimit(t *testing.T) {
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
			store := directoryStore()
			svc := NewUserService(store, nil)

			if _, _, err := svc.ListDirectory(context.Background(), "", tt.limit, 0); err != nil {
				t.Fatalf("ListDirectory: %v", err)
			}
			if store.directoryLimit != tt.want {
				t.Errorf("repository asked for limit %d, want %d", store.directoryLimit, tt.want)
			}
		})
	}
}

// A negative offset is not an overreach with an obvious intent, it is a caller
// that has lost track of where it is, so it is refused rather than bounded.
func TestUserService_ListDirectory_RejectsANegativeOffset(t *testing.T) {
	svc := NewUserService(directoryStore(), nil)

	_, _, err := svc.ListDirectory(context.Background(), "", 25, -1)
	if !errors.Is(err, ErrValidation) {
		t.Errorf("error = %v, want ErrValidation", err)
	}
}

func TestUserService_ListDirectory_RejectsAnOverlongQuery(t *testing.T) {
	svc := NewUserService(directoryStore(), nil)

	_, _, err := svc.ListDirectory(context.Background(), strings.Repeat("a", maxDirectorySearchLength+1), 25, 0)
	if !errors.Is(err, ErrValidation) {
		t.Errorf("error = %v, want ErrValidation", err)
	}

	// The boundary itself is fine — the cap is a guard, not a policy on names.
	if _, _, err := svc.ListDirectory(context.Background(), strings.Repeat("a", maxDirectorySearchLength), 25, 0); err != nil {
		t.Errorf("a query at the limit was rejected: %v", err)
	}
}

// The count is measured in runes, so a multi-byte name is not refused for being
// long in bytes.
func TestUserService_ListDirectory_CountsQueryLengthInRunes(t *testing.T) {
	svc := NewUserService(directoryStore(), nil)

	// Three bytes each, so this is well past the cap in bytes and exactly at it
	// in characters.
	query := strings.Repeat("あ", maxDirectorySearchLength)
	if _, _, err := svc.ListDirectory(context.Background(), query, 25, 0); err != nil {
		t.Errorf("a %d-character query was rejected: %v", maxDirectorySearchLength, err)
	}
}

// Whitespace around a search term comes from a text box, not from intent. It is
// trimmed before it reaches the repository, so "  ali  " and "ali" find the
// same neighbours and "   " lists everyone rather than nobody.
func TestUserService_ListDirectory_TrimsTheQuery(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"surrounding whitespace is dropped", "  ali  ", "ali"},
		{"whitespace only becomes the empty query", "   ", ""},
		{"inner spaces are kept", " ada l ", "ada l"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := directoryStore()
			svc := NewUserService(store, nil)

			if _, _, err := svc.ListDirectory(context.Background(), tt.in, 25, 0); err != nil {
				t.Fatalf("ListDirectory: %v", err)
			}
			if store.directoryQuery != tt.want {
				t.Errorf("repository asked for query %q, want %q", store.directoryQuery, tt.want)
			}
		})
	}
}

// total is the size of the whole match, not of the page. A pager that read the
// page length would think there was nothing after the first screen.
func TestUserService_ListDirectory_TotalCountsEveryMatch(t *testing.T) {
	svc := NewUserService(directoryStore(
		directoryUser("u1", "Ada", domain.RoleMember, 1),
		directoryUser("u2", "Alice", domain.RolePending, 2),
		directoryUser("u3", "Bob", domain.RoleModerator, 3),
	), nil)

	users, total, err := svc.ListDirectory(context.Background(), "", 2, 0)
	if err != nil {
		t.Fatalf("ListDirectory: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("page holds %d users, want 2", len(users))
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
}

// Pending residents are in the directory on purpose: being findable is how they
// acquire the vouch that stops them being pending. Banned and deactivated
// accounts are not.
func TestUserService_ListDirectory_Population(t *testing.T) {
	banned := directoryUser("u4", "Mallory", domain.RoleBanned, 4)
	departed := directoryUser("u5", "Gone", domain.RoleMember, 5)
	departed.IsActive = false

	svc := NewUserService(directoryStore(
		directoryUser("u1", "Ada", domain.RoleMember, 1),
		directoryUser("u2", "Alice", domain.RolePending, 2),
		directoryUser("u3", "Bob", domain.RoleCouncil, 3),
		banned,
		departed,
	), nil)

	users, total, err := svc.ListDirectory(context.Background(), "", 25, 0)
	if err != nil {
		t.Fatalf("ListDirectory: %v", err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3 (banned and deactivated excluded)", total)
	}
	for _, u := range users {
		if u.ID == banned.ID {
			t.Error("a banned account is in the directory")
		}
		if u.ID == departed.ID {
			t.Error("a deactivated account is in the directory")
		}
	}
}

// A failure to read is a failure to answer. Reporting an empty directory would
// tell a pending resident there is nobody in town to vouch for them.
func TestUserService_ListDirectory_ReportsRepositoryFailures(t *testing.T) {
	t.Run("the listing", func(t *testing.T) {
		store := directoryStore()
		store.directoryErr = errors.New("db down")
		svc := NewUserService(store, nil)

		if _, _, err := svc.ListDirectory(context.Background(), "", 25, 0); err == nil {
			t.Error("a failed listing was reported as an empty directory")
		}
	})

	t.Run("the count", func(t *testing.T) {
		store := directoryStore()
		store.countDirectoryErr = errors.New("db down")
		svc := NewUserService(store, nil)

		if _, _, err := svc.ListDirectory(context.Background(), "", 25, 0); err == nil {
			t.Error("a failed count was reported as a total of zero")
		}
	})
}

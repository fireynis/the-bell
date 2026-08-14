package service

import (
	"context"
	"sort"
	"strings"

	"github.com/fireynis/the-bell/internal/domain"
)

// fakeUserStore is the one in-memory user store shared by the service tests.
// Four near-identical copies of this map used to live in user_test.go,
// approval_test.go, moderation_action_test.go and vouch_test.go; a change to
// user lookup semantics had to be made in four places and could silently
// disagree between them.
//
// It satisfies UserRepository, ActionUserLookup, ApprovalUserRepository and
// UserGetter, so any service taking one of those can be handed this.
type fakeUserStore struct {
	users    map[string]*domain.User // keyed by ID
	byKratos map[string]*domain.User // keyed by KratosIdentityID

	// Error injection. Each field short-circuits the matching method so tests
	// can drive the failure branches without a real repository.
	createErr         error
	getByKratosErr    error
	updateRoleErr     error
	countErr          error
	directoryErr      error
	countDirectoryErr error

	// What the last directory listing was asked for, so a test can assert the
	// service's clamping rather than inferring it from the page it got back.
	directoryQuery  string
	directoryLimit  int
	directoryOffset int
}

func newFakeUserStore() *fakeUserStore {
	return &fakeUserStore{
		users:    make(map[string]*domain.User),
		byKratos: make(map[string]*domain.User),
	}
}

// add registers a user under both keys, mirroring what CreateUser would do.
func (f *fakeUserStore) add(u *domain.User) {
	f.users[u.ID] = u
	if u.KratosIdentityID != "" {
		f.byKratos[u.KratosIdentityID] = u
	}
}

// --- UserRepository ---

func (f *fakeUserStore) CreateUser(_ context.Context, user *domain.User) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.users[user.ID] = user
	f.byKratos[user.KratosIdentityID] = user
	return nil
}

func (f *fakeUserStore) GetUserByID(_ context.Context, id string) (*domain.User, error) {
	u, ok := f.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	return u, nil
}

func (f *fakeUserStore) GetUserByKratosID(_ context.Context, kratosID string) (*domain.User, error) {
	if f.getByKratosErr != nil {
		return nil, f.getByKratosErr
	}
	u, ok := f.byKratos[kratosID]
	if !ok {
		return nil, ErrNotFound
	}
	return u, nil
}

func (f *fakeUserStore) UpdateUserProfile(_ context.Context, id, displayName, bio, avatarURL string) (*domain.User, error) {
	u, ok := f.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	u.DisplayName = displayName
	u.Bio = bio
	u.AvatarURL = avatarURL
	return u, nil
}

// directoryMatches applies the same filter both directory methods must agree
// on: active, not banned, and matching the substring when one is given.
//
// The comparison is a plain lowercase substring rather than an escaped LIKE.
// Escaping is the SQL adapter's business — see postgres.escapeLikePattern — and
// a fake that reimplemented it would be asserting against its own copy of the
// rule instead of the service's behaviour.
func (f *fakeUserStore) directoryMatches(query string) []*domain.User {
	query = strings.ToLower(query)
	var matched []*domain.User
	for _, u := range f.users {
		if !u.IsActive || u.Role == domain.RoleBanned {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(u.DisplayName), query) {
			continue
		}
		matched = append(matched, u)
	}
	// Newest first, as the query orders it. The map iteration above is random,
	// so without this the page a test sees would vary run to run.
	sort.Slice(matched, func(i, j int) bool {
		if !matched[i].JoinedAt.Equal(matched[j].JoinedAt) {
			return matched[i].JoinedAt.After(matched[j].JoinedAt)
		}
		return matched[i].ID > matched[j].ID
	})
	return matched
}

func (f *fakeUserStore) ListDirectoryUsers(_ context.Context, query string, limit, offset int) ([]*domain.User, error) {
	if f.directoryErr != nil {
		return nil, f.directoryErr
	}
	f.directoryQuery, f.directoryLimit, f.directoryOffset = query, limit, offset

	matched := f.directoryMatches(query)
	if offset >= len(matched) {
		return nil, nil
	}
	end := min(offset+limit, len(matched))
	return matched[offset:end], nil
}

func (f *fakeUserStore) CountDirectoryUsers(_ context.Context, query string) (int64, error) {
	if f.countDirectoryErr != nil {
		return 0, f.countDirectoryErr
	}
	return int64(len(f.directoryMatches(query))), nil
}

// --- ApprovalUserRepository / UserGetter ---

func (f *fakeUserStore) ListPendingUsers(_ context.Context) ([]*domain.User, error) {
	var pending []*domain.User
	for _, u := range f.users {
		if u.Role == domain.RolePending && u.IsActive {
			pending = append(pending, u)
		}
	}
	return pending, nil
}

func (f *fakeUserStore) CountActiveMembers(_ context.Context) (int64, error) {
	if f.countErr != nil {
		return 0, f.countErr
	}
	var count int64
	for _, u := range f.users {
		if u.IsActive && (u.Role == domain.RoleMember || u.Role == domain.RoleModerator || u.Role == domain.RoleCouncil) {
			count++
		}
	}
	return count, nil
}

func (f *fakeUserStore) UpdateUserRole(_ context.Context, id string, role domain.Role) error {
	if f.updateRoleErr != nil {
		return f.updateRoleErr
	}
	u, ok := f.users[id]
	if !ok {
		return ErrNotFound
	}
	u.Role = role
	return nil
}

// Compile-time proof that one fake covers every user-shaped dependency.
var (
	_ UserRepository         = (*fakeUserStore)(nil)
	_ ActionUserLookup       = (*fakeUserStore)(nil)
	_ ApprovalUserRepository = (*fakeUserStore)(nil)
	_ UserGetter             = (*fakeUserStore)(nil)
)

// Transitional aliases. bootstrap_test.go and moderation_action_history_test.go
// still name the old types; these keep them compiling against the single
// implementation above and should be deleted once those files are updated.
type (
	mockUserRepo         = fakeUserStore
	mockActionUserLookup = fakeUserStore
	mockApprovalUserRepo = fakeUserStore
)

func newMockUserRepo() *fakeUserStore         { return newFakeUserStore() }
func newMockActionUserLookup() *fakeUserStore { return newFakeUserStore() }
func newMockApprovalUserRepo() *fakeUserStore { return newFakeUserStore() }

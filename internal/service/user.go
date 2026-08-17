package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/google/uuid"
)

// UserRepository abstracts user persistence using domain types.
type UserRepository interface {
	CreateUser(ctx context.Context, user *domain.User) error
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
	GetUserByKratosID(ctx context.Context, kratosID string) (*domain.User, error)
	UpdateUserProfile(ctx context.Context, id, displayName, bio, avatarURL string) (*domain.User, error)
	// ListDirectoryUsers and CountDirectoryUsers page and size the same
	// population, so an implementation must apply the same filter to both. query
	// is a plain substring — escaping whatever the storage engine reads as
	// syntax is the implementation's job.
	ListDirectoryUsers(ctx context.Context, query string, limit, offset int) ([]*domain.User, error)
	CountDirectoryUsers(ctx context.Context, query string) (int64, error)
	// SetUserResidencyClaim stores the resident's statement of where they live.
	// The empty string clears it.
	SetUserResidencyClaim(ctx context.Context, id, claim string) error
}

// UserService orchestrates user business logic.
type UserService struct {
	repo UserRepository
	now  func() time.Time
}

func NewUserService(repo UserRepository, clock func() time.Time) *UserService {
	if clock == nil {
		clock = time.Now
	}
	return &UserService{
		repo: repo,
		now:  clock,
	}
}

// FindOrCreate looks up a user by Kratos identity ID. If no local user exists,
// it auto-provisions one as pending with trust 50.0, seeded with displayName.
//
// The second return reports whether this call created the user. It exists
// because provisioning is the one moment something outside this service may
// need to act on — invitation redemption is the current caller — and the
// alternative, inferring it from a zero-valued JoinedAt or a role of pending,
// would be reading a guess off the row.
//
// displayName is the identity's `name` trait and applies to creation only. An
// existing user is returned untouched, because by then the name is theirs to
// manage: UpdateProfile lets them change it in-app, and re-applying the trait
// on every request would silently undo that edit at the next page load.
//
// An empty displayName still creates the user. A town where nobody can sign in
// because a trait is missing is far worse than one where a name has to be
// filled in later, and callers with no identity to read (fixtures, tooling)
// legitimately have nothing to pass.
//
// The identity's email is deliberately NOT a parameter and is not stored. It
// travels alongside this call rather than through it (see
// middleware.resolveUser), because the address belongs to Kratos: copying it
// into users would create a second, immediately-stale copy of the one fact
// Kratos is authoritative for, and every read of it would have to ask which
// copy was right. The one thing that needs the address — matching a pending
// user to their invitation — asks the session for it at the moment it needs it.
func (s *UserService) FindOrCreate(ctx context.Context, kratosID, displayName string) (*domain.User, bool, error) {
	user, err := s.repo.GetUserByKratosID(ctx, kratosID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, false, fmt.Errorf("looking up user by kratos id: %w", err)
	}
	if user != nil {
		return user, false, nil
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, false, fmt.Errorf("generating user id: %w", err)
	}

	now := s.now()
	user = &domain.User{
		ID:               id.String(),
		KratosIdentityID: kratosID,
		DisplayName:      strings.TrimSpace(displayName),
		TrustScore:       50.0,
		Role:             domain.RolePending,
		IsActive:         true,
		JoinedAt:         now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := s.repo.CreateUser(ctx, user); err != nil {
		return nil, false, fmt.Errorf("creating user: %w", err)
	}

	return user, true, nil
}

// FindByKratosID satisfies the middleware.UserFinder interface by delegating
// to FindOrCreate.
func (s *UserService) FindByKratosID(ctx context.Context, kratosID, displayName string) (*domain.User, bool, error) {
	return s.FindOrCreate(ctx, kratosID, displayName)
}

// GetByID retrieves a user by their primary ID.
func (s *UserService) GetByID(ctx context.Context, id string) (*domain.User, error) {
	return s.repo.GetUserByID(ctx, id)
}

const (
	maxDisplayNameLength = 100
	maxBioLength         = 500

	// maxResidencyClaimLength bounds the residency claim, in runes.
	//
	// Three hundred is room for a street address with a unit number, a
	// landmark, and a sentence of context ("the blue house behind the old
	// mill") — which is the form a claim actually takes in a town where the
	// council recognises places rather than validates them. It is counted in
	// runes rather than bytes so a claim written in a non-Latin script gets the
	// same three hundred characters as one written in English.
	maxResidencyClaimLength = 300
)

const (
	// DirectoryDefaultLimit and DirectoryMaxLimit bound one page of the member
	// directory. 25 is a screenful of neighbours; 100 is the same ceiling the
	// feed uses, and exists so one request cannot ask for the whole town.
	//
	// The council's approval queue pages on the same numbers — see
	// ApprovalService.ListPending. Two searchable listings of users, both read
	// by a person scanning names, and a queue that held a different number of
	// rows per page than the directory would be an arbitrary difference for a
	// client to carry.
	DirectoryDefaultLimit = 25
	DirectoryMaxLimit     = 100

	// maxDirectorySearchLength caps the search term. It is generous next to
	// maxDisplayNameLength — nothing longer can match a name — and is here to
	// keep a pathological string out of a LIKE scan, not to police input.
	maxDirectorySearchLength = 100
)

// boundUserListing applies the shared rules for paging a searchable listing of
// users, returning the trimmed query and the bounded page.
//
// A page is bounded rather than rejected when the caller overreaches: a limit
// of zero or less means "use the default" and one above the ceiling is clamped,
// matching how the feed treats ?limit=. A negative offset is different — it is
// not an overreach with an obvious intent, it is a caller that has lost track of
// where it is — so it is refused.
//
// It is one function rather than a rule per listing because the directory and
// the approval queue publish the same bounds, and a limit that clamped in one
// and rejected in the other would be a difference nobody chose.
func boundUserListing(query string, limit, offset int) (string, int, int, error) {
	query = strings.TrimSpace(query)
	if utf8.RuneCountInString(query) > maxDirectorySearchLength {
		return "", 0, 0, fmt.Errorf("%w: search query exceeds %d characters", ErrValidation, maxDirectorySearchLength)
	}
	if offset < 0 {
		return "", 0, 0, fmt.Errorf("%w: offset must not be negative", ErrValidation)
	}
	if limit <= 0 {
		limit = DirectoryDefaultLimit
	}
	if limit > DirectoryMaxLimit {
		limit = DirectoryMaxLimit
	}
	return query, limit, offset, nil
}

// ListDirectory returns one page of the member directory and the total number
// of people matching the search, so a caller can page through them.
//
// The bounds on the page and the search term are boundUserListing's; see there
// for why an overlong limit is clamped and a negative offset is not.
//
// The count is a second query rather than a window function over the page: at
// an offset past the end the page is empty and there would be no row to carry
// the total on, which is exactly when a pager needs it most.
func (s *UserService) ListDirectory(ctx context.Context, query string, limit, offset int) ([]*domain.User, int, error) {
	query, limit, offset, err := boundUserListing(query, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	users, err := s.repo.ListDirectoryUsers(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("listing directory users: %w", err)
	}

	total, err := s.repo.CountDirectoryUsers(ctx, query)
	if err != nil {
		return nil, 0, fmt.Errorf("counting directory users: %w", err)
	}

	return users, int(total), nil
}

// UpdateProfile updates a user's display name, bio, and avatar URL.
func (s *UserService) UpdateProfile(ctx context.Context, id, displayName, bio, avatarURL string) (*domain.User, error) {
	if strings.TrimSpace(displayName) == "" {
		return nil, fmt.Errorf("%w: display name must not be empty", ErrValidation)
	}
	if len(displayName) > maxDisplayNameLength {
		return nil, fmt.Errorf("%w: display name exceeds %d characters", ErrValidation, maxDisplayNameLength)
	}
	if len(bio) > maxBioLength {
		return nil, fmt.Errorf("%w: bio exceeds %d characters", ErrValidation, maxBioLength)
	}

	return s.repo.UpdateUserProfile(ctx, id, displayName, bio, avatarURL)
}

// UpdateResidencyClaim stores what a resident says about where they live, for
// the council to read while deciding whether to approve them.
//
// It is separate from UpdateProfile rather than a fourth field on it, because
// the two have different readers. A profile is public: display name, bio and
// avatar are what the town sees. A residency claim is for the reviewing council
// alone, and folding it into the profile write would mean every profile edit
// round-tripped an address through a form the whole town can read, and that a
// resident updating their bio had to resend their address or lose it.
//
// The claim is trimmed and length-checked and nothing else. Nothing here — and
// nothing anywhere in the codebase — tries to decide whether it is true: it is
// an attestation a human reviews, so a validator that rejected "the blue house
// behind the old mill" for not looking like an address would be refusing the
// most useful thing a neighbour can say.
//
// An empty claim is not an error. Clearing what you said about where you live
// has to be as easy as saying it, and a resident who is having second thoughts
// about a pending application should not have to ask an administrator.
func (s *UserService) UpdateResidencyClaim(ctx context.Context, id, claim string) error {
	claim = strings.TrimSpace(claim)
	if utf8.RuneCountInString(claim) > maxResidencyClaimLength {
		return fmt.Errorf("%w: residency claim exceeds %d characters", ErrValidation, maxResidencyClaimLength)
	}
	if err := s.repo.SetUserResidencyClaim(ctx, id, claim); err != nil {
		return fmt.Errorf("saving residency claim: %w", err)
	}
	return nil
}

// PendingUserLister and ActiveUserLister are the two rosters the backfill walks.
//
// They are separate one-method interfaces because the two listings live on
// different repositories — UserRepo owns the pending queue, RoleCheckerRepo owns
// the active roster — and a single combined interface would force one of them to
// grow a method it has no other use for. Between them they cover every user who
// can sign in or is waiting to be let in; a banned or deactivated account is
// deliberately out of reach, since nothing renders its name.
type PendingUserLister interface {
	ListPendingUsers(ctx context.Context) ([]*domain.User, error)
}

type ActiveUserLister interface {
	ListActiveNonBannedUsers(ctx context.Context) ([]*domain.User, error)
}

// IdentityDirectory reads a display name out of the identity provider.
//
// Returning ("", nil) means the identity exists but carries no usable name —
// a schema without the trait, or one holding a non-string — which the backfill
// counts as a skip rather than a failure. Only a failed lookup errors.
type IdentityDirectory interface {
	IdentityDisplayName(ctx context.Context, kratosID string) (string, error)
}

// DisplayNameChange is one name the backfill wrote, or would have written under
// --dry-run.
type DisplayNameChange struct {
	UserID      string
	KratosID    string
	DisplayName string
	// Truncated reports that the identity's name was longer than the app allows
	// and DisplayName holds the shortened form that was stored.
	Truncated bool
}

// DisplayNameBackfillResult counts what one backfill run did. The four skip and
// failure counts are disjoint, and together with Updated they account for every
// user Scanned.
type DisplayNameBackfillResult struct {
	Scanned        int
	Updated        int
	SkippedNamed   int
	SkippedNoTrait int
	Errors         int
	Changes        []DisplayNameChange
}

// DisplayNameBackfill fills in display names that were never captured.
//
// The `name` trait is applied at account creation only (see FindOrCreate), so
// every user provisioned before that sync landed has an empty display name and
// no way to acquire one but editing their own profile. That is worst for the
// council's approval queue, where the people being judged are exactly the ones
// who have not signed in enough to notice a blank name.
//
// It reads the trait back out of Kratos and writes it through UserService, so
// the same validation the profile endpoint applies governs the backfill too. It
// is idempotent: a user who already has a name is skipped, never overwritten.
type DisplayNameBackfill struct {
	pending    PendingUserLister
	active     ActiveUserLister
	identities IdentityDirectory
	users      *UserService
	logger     *slog.Logger
}

func NewDisplayNameBackfill(pending PendingUserLister, active ActiveUserLister, identities IdentityDirectory, users *UserService, logger *slog.Logger) *DisplayNameBackfill {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &DisplayNameBackfill{
		pending:    pending,
		active:     active,
		identities: identities,
		users:      users,
		logger:     logger,
	}
}

// Run backfills every reachable user with an empty display name.
//
// dryRun reports what would change and writes nothing. Either way a single
// user's failure — an identity Kratos will not return, a write that is
// rejected — is counted and logged, never fatal: the run exists to fix as many
// blank names as it can, and a town where one identity is missing should still
// have the rest of its names filled in.
func (b *DisplayNameBackfill) Run(ctx context.Context, dryRun bool) (*DisplayNameBackfillResult, error) {
	users, err := b.roster(ctx)
	if err != nil {
		return nil, err
	}

	result := &DisplayNameBackfillResult{Scanned: len(users)}

	for _, u := range users {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		b.backfillOne(ctx, u, dryRun, result)
	}

	return result, nil
}

// roster gathers the users to consider, pending queue first, de-duplicated by
// ID. The two listings are disjoint today — one selects role = 'pending', the
// other excludes it — but de-duplicating means a query that later widens cannot
// make the backfill update somebody twice or double-count them.
func (b *DisplayNameBackfill) roster(ctx context.Context) ([]*domain.User, error) {
	pending, err := b.pending.ListPendingUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing pending users: %w", err)
	}
	active, err := b.active.ListActiveNonBannedUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing active users: %w", err)
	}

	seen := make(map[string]bool, len(pending)+len(active))
	roster := make([]*domain.User, 0, len(pending)+len(active))
	for _, listing := range [][]*domain.User{pending, active} {
		for _, u := range listing {
			if u == nil || seen[u.ID] {
				continue
			}
			seen[u.ID] = true
			roster = append(roster, u)
		}
	}
	return roster, nil
}

// backfillOne resolves and applies the name for a single user, recording the
// outcome in result.
func (b *DisplayNameBackfill) backfillOne(ctx context.Context, u *domain.User, dryRun bool, result *DisplayNameBackfillResult) {
	// A name of nothing but whitespace is as blank as an empty one, and is what
	// a trait of "   " produced back when it was stored untrimmed.
	if strings.TrimSpace(u.DisplayName) != "" {
		result.SkippedNamed++
		return
	}

	if u.KratosIdentityID == "" {
		// Fixtures and tooling can create a user with no identity behind it;
		// there is nowhere to read a name from, so it is a skip, not a failure.
		b.logger.Warn("backfill: user has no kratos identity", "user_id", u.ID)
		result.SkippedNoTrait++
		return
	}

	name, err := b.identities.IdentityDisplayName(ctx, u.KratosIdentityID)
	if err != nil {
		b.logger.Error("backfill: fetching identity failed", "user_id", u.ID, "kratos_id", u.KratosIdentityID, "error", err)
		result.Errors++
		return
	}

	name = strings.TrimSpace(name)
	if name == "" {
		b.logger.Info("backfill: identity has no name trait", "user_id", u.ID, "kratos_id", u.KratosIdentityID)
		result.SkippedNoTrait++
		return
	}

	name, truncated := truncateDisplayName(name)
	if truncated {
		b.logger.Warn("backfill: display name truncated to fit",
			"user_id", u.ID, "kratos_id", u.KratosIdentityID, "limit", maxDisplayNameLength, "stored", name)
	}

	change := DisplayNameChange{
		UserID:      u.ID,
		KratosID:    u.KratosIdentityID,
		DisplayName: name,
		Truncated:   truncated,
	}

	if dryRun {
		result.Updated++
		result.Changes = append(result.Changes, change)
		return
	}

	// Bio and avatar are passed back unchanged: UpdateProfile writes all three
	// columns, so reading them off the listed user is what keeps this a
	// display-name backfill rather than a profile wipe.
	if _, err := b.users.UpdateProfile(ctx, u.ID, name, u.Bio, u.AvatarURL); err != nil {
		b.logger.Error("backfill: updating display name failed", "user_id", u.ID, "error", err)
		result.Errors++
		return
	}

	b.logger.Info("backfill: display name set", "user_id", u.ID, "display_name", name)
	result.Updated++
	result.Changes = append(result.Changes, change)
}

// truncateDisplayName caps a name at the app's limit, reporting whether it had
// to cut.
//
// The Kratos identity schema allows a longer name than the app does, so a name
// that is legal upstream can be rejected by UpdateProfile — which would turn a
// user the backfill exists to help into an error. The cut lands on a rune
// boundary: maxDisplayNameLength counts bytes, and slicing mid-rune would store
// a replacement character as the last thing in somebody's name.
func truncateDisplayName(name string) (string, bool) {
	if len(name) <= maxDisplayNameLength {
		return name, false
	}
	cut := maxDisplayNameLength
	for cut > 0 && !utf8.RuneStart(name[cut]) {
		cut--
	}
	return strings.TrimRight(name[:cut], " \t\n\r"), true
}

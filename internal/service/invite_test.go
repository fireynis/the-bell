package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/mail"
)

// fakeInviteRepo is an in-memory InviteRepository.
//
// Liveness, the one-live-invite rule and the exactly-once consume are all
// re-implemented here against the same clock the service uses, rather than
// stubbed to fixed answers. That is what makes the tests below able to say
// anything about ordering: a consume that races, an expired row that gets
// reaped, a second invitation refused because the first is still open — none
// of those are observable against a repository that only returns what it was
// told to.
type fakeInviteRepo struct {
	invites map[string]*domain.Invite
	// hashes maps token hash to invite id.
	hashes map[string]string

	createErr   error
	listErr     error
	consumeErr  error
	reapErr     error
	countErr    error
	byEmailErr  error
	blockingErr error

	reaped   []string
	consumed []string
}

func newFakeInviteRepo() *fakeInviteRepo {
	return &fakeInviteRepo{
		invites: make(map[string]*domain.Invite),
		hashes:  make(map[string]string),
	}
}

func (f *fakeInviteRepo) CreateInvite(_ context.Context, invite *domain.Invite, tokenHash string) error {
	if f.createErr != nil {
		return f.createErr
	}
	// The unique index, in miniature: one unconsumed, unrevoked row per
	// address, expiry not considered.
	for _, existing := range f.invites {
		if strings.EqualFold(existing.Email, invite.Email) &&
			existing.ConsumedAt == nil && existing.RevokedAt == nil {
			return ErrValidation
		}
	}
	stored := *invite
	f.invites[invite.ID] = &stored
	f.hashes[tokenHash] = invite.ID
	return nil
}

func (f *fakeInviteRepo) GetLiveInviteByTokenHash(_ context.Context, tokenHash string, now time.Time) (*domain.Invite, error) {
	id, ok := f.hashes[tokenHash]
	if !ok {
		return nil, ErrNotFound
	}
	invite := f.invites[id]
	if invite == nil || !invite.IsLive(now) {
		return nil, ErrNotFound
	}
	return invite, nil
}

func (f *fakeInviteRepo) GetLiveInviteByEmail(_ context.Context, email string, now time.Time) (*domain.Invite, error) {
	if f.byEmailErr != nil {
		return nil, f.byEmailErr
	}
	for _, invite := range f.invites {
		if strings.EqualFold(invite.Email, email) && invite.IsLive(now) {
			return invite, nil
		}
	}
	return nil, ErrNotFound
}

func (f *fakeInviteRepo) GetBlockingInviteByEmail(_ context.Context, email string) (*domain.Invite, error) {
	if f.blockingErr != nil {
		return nil, f.blockingErr
	}
	for _, invite := range f.invites {
		if strings.EqualFold(invite.Email, email) && invite.ConsumedAt == nil && invite.RevokedAt == nil {
			return invite, nil
		}
	}
	return nil, ErrNotFound
}

func (f *fakeInviteRepo) CountConsumedInvitesByEmail(_ context.Context, email string) (int64, error) {
	var count int64
	for _, invite := range f.invites {
		if strings.EqualFold(invite.Email, email) && invite.ConsumedAt != nil {
			count++
		}
	}
	return count, nil
}

func (f *fakeInviteRepo) CountInvitesByInviterSince(_ context.Context, inviterID string, since time.Time) (int64, error) {
	if f.countErr != nil {
		return 0, f.countErr
	}
	var count int64
	for _, invite := range f.invites {
		if invite.InviterID == inviterID && !invite.CreatedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

func (f *fakeInviteRepo) ListInvitesByInviter(_ context.Context, inviterID string) ([]*domain.Invite, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []*domain.Invite
	for _, invite := range f.invites {
		if invite.InviterID == inviterID {
			out = append(out, invite)
		}
	}
	return out, nil
}

func (f *fakeInviteRepo) RevokeInvite(_ context.Context, id, inviterID string, now time.Time) error {
	invite, ok := f.invites[id]
	if !ok || invite.InviterID != inviterID || !invite.IsLive(now) {
		return ErrNotFound
	}
	invite.RevokedAt = &now
	return nil
}

func (f *fakeInviteRepo) ReapInvite(_ context.Context, id string, now time.Time) error {
	if f.reapErr != nil {
		return f.reapErr
	}
	invite, ok := f.invites[id]
	if !ok || invite.ConsumedAt != nil || invite.RevokedAt != nil {
		return nil
	}
	invite.RevokedAt = &now
	f.reaped = append(f.reaped, id)
	return nil
}

func (f *fakeInviteRepo) ConsumeInvite(_ context.Context, id, userID string, now time.Time) (*domain.Invite, error) {
	if f.consumeErr != nil {
		return nil, f.consumeErr
	}
	invite, ok := f.invites[id]
	if !ok || !invite.IsLive(now) {
		return nil, ErrNotFound
	}
	invite.ConsumedAt = &now
	invite.ConsumedBy = userID
	f.consumed = append(f.consumed, id)
	return invite, nil
}

// seed stores an invitation directly, for the states Create cannot produce.
func (f *fakeInviteRepo) seed(invite *domain.Invite, tokenHash string) *domain.Invite {
	stored := *invite
	f.invites[invite.ID] = &stored
	if tokenHash != "" {
		f.hashes[tokenHash] = invite.ID
	}
	return &stored
}

// fakeInviteVoucher records the redemption vouches the service asks for.
type fakeInviteVoucher struct {
	calls [][2]string
	err   error
	users *fakeUserStore
}

func (f *fakeInviteVoucher) VouchFromInvite(_ context.Context, voucherID, voucheeID string) (*domain.Vouch, error) {
	f.calls = append(f.calls, [2]string{voucherID, voucheeID})
	if f.err != nil {
		return nil, f.err
	}
	// Promotion is the real service's job; the fake does it so that tests can
	// assert on the user the middleware would go on to serve.
	if f.users != nil {
		if u, ok := f.users.users[voucheeID]; ok && u.Role == domain.RolePending {
			u.Role = domain.RoleMember
		}
	}
	return &domain.Vouch{ID: "vouch-" + voucheeID, VoucherID: voucherID, VoucheeID: voucheeID}, nil
}

// recordingMailer captures what would have been sent.
type recordingMailer struct {
	sent []mail.Message
	err  error
}

func (m *recordingMailer) Send(_ context.Context, msg mail.Message) error {
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, msg)
	return nil
}

type inviteHarness struct {
	svc      *InviteService
	invites  *fakeInviteRepo
	vouches  *mockVouchRepo
	voucher  *fakeInviteVoucher
	users    *fakeUserStore
	config   *mockConfigRepo
	mailer   *recordingMailer
	inviter  *domain.User
	council  *domain.User
	newcomer *domain.User
}

// newInviteHarness builds a town with one member who may vouch, one council
// member, and one pending newcomer.
func newInviteHarness(t *testing.T) *inviteHarness {
	t.Helper()

	users := newFakeUserStore()
	inviter := &domain.User{ID: "inviter-1", DisplayName: "Ana", Role: domain.RoleMember, IsActive: true, TrustScore: 75}
	council := &domain.User{ID: "council-1", DisplayName: "Cass", Role: domain.RoleCouncil, IsActive: true, TrustScore: 100}
	newcomer := &domain.User{ID: "newcomer-1", Role: domain.RolePending, IsActive: true, TrustScore: 50}
	users.add(inviter)
	users.add(council)
	users.add(newcomer)

	invites := newFakeInviteRepo()
	vouches := newMockVouchRepo()
	voucher := &fakeInviteVoucher{users: users}
	config := newMockConfigRepo()
	mailer := &recordingMailer{}

	svc := NewInviteService(invites, vouches, voucher, users, config, discardLogger(), fixedClock)
	svc.SetMailer(mailer)
	svc.SetPublicURL("https://bell.example.test")
	svc.SetTownName("Bellville")

	return &inviteHarness{
		svc: svc, invites: invites, vouches: vouches, voucher: voucher,
		users: users, config: config, mailer: mailer,
		inviter: inviter, council: council, newcomer: newcomer,
	}
}

func mustCreate(t *testing.T, h *inviteHarness, inviter *domain.User, email, note string) *InviteCreation {
	t.Helper()
	creation, err := h.svc.Create(context.Background(), inviter, email, note)
	if err != nil {
		t.Fatalf("Create(%q) unexpected error: %v", email, err)
	}
	return creation
}

// --- Create ---

func TestInviteService_Create_StoresTheInvitationAndReturnsTheLinkOnce(t *testing.T) {
	h := newInviteHarness(t)

	creation := mustCreate(t, h, h.inviter, "  Newcomer@Example.COM ", "  see you at the market  ")

	if creation.Invite.Email != "newcomer@example.com" {
		t.Errorf("stored email = %q, want it trimmed and lowercased", creation.Invite.Email)
	}
	if creation.Invite.Note != "see you at the market" {
		t.Errorf("note = %q, want it trimmed", creation.Invite.Note)
	}
	if creation.Invite.ExpiresAt != fixedNow.Add(InviteTTL) {
		t.Errorf("expires_at = %v, want %v", creation.Invite.ExpiresAt, fixedNow.Add(InviteTTL))
	}
	if got, want := creation.Invite.Status(fixedNow), domain.InviteOpen; got != want {
		t.Errorf("status = %q, want %q", got, want)
	}

	const prefix = "https://bell.example.test/auth/registration?invite="
	if !strings.HasPrefix(creation.URL, prefix) {
		t.Fatalf("invite_url = %q, want prefix %q", creation.URL, prefix)
	}
	rawToken := strings.TrimPrefix(creation.URL, prefix)
	if rawToken == "" {
		t.Fatal("invite_url carries no token")
	}

	// The raw token is nowhere in storage; only its hash is, and the hash is
	// what the lookup takes.
	for _, stored := range h.invites.invites {
		if strings.Contains(stored.Email+stored.Note+stored.ID, rawToken) {
			t.Error("the raw token appears in a stored field")
		}
	}
	if _, found := h.invites.hashes[rawToken]; found {
		t.Error("the raw token is stored as its own key; only the hash may be")
	}
	if _, found := h.invites.hashes[hashInviteToken(rawToken)]; !found {
		t.Error("the token hash was not stored")
	}
}

func TestInviteService_Create_RefusesAMemberWhoCouldNotVouch(t *testing.T) {
	h := newInviteHarness(t)
	tests := []struct {
		name string
		user *domain.User
	}{
		{"below the trust threshold", &domain.User{ID: "u", Role: domain.RoleMember, IsActive: true, TrustScore: 59}},
		{"suspended", &domain.User{ID: "u", Role: domain.RoleMember, IsActive: false, TrustScore: 90}},
		{"still pending", &domain.User{ID: "u", Role: domain.RolePending, IsActive: true, TrustScore: 90}},
		{"banned", &domain.User{ID: "u", Role: domain.RoleBanned, IsActive: true, TrustScore: 90}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.svc.Create(context.Background(), tt.user, "someone@example.com", "")
			if !errors.Is(err, ErrForbidden) {
				t.Errorf("Create() error = %v, want ErrForbidden", err)
			}
		})
	}
}

func TestInviteService_Create_RejectsImplausibleAddresses(t *testing.T) {
	h := newInviteHarness(t)
	tests := []string{
		"",
		"   ",
		"nobody",
		"nobody@",
		"@example.com",
		"nobody@localhost",
		"two@at@example.com",
		"has space@example.com",
		"Ana <ana@example.com>",
		"nobody@.example.com",
		"nobody@example..com",
		"nobody@example.com.",
		strings.Repeat("a", 320) + "@example.com",
	}

	for _, email := range tests {
		t.Run(email, func(t *testing.T) {
			_, err := h.svc.Create(context.Background(), h.inviter, email, "")
			if !errors.Is(err, ErrValidation) {
				t.Errorf("Create(%q) error = %v, want ErrValidation", email, err)
			}
		})
	}
}

func TestInviteService_Create_AcceptsOrdinaryAddresses(t *testing.T) {
	tests := []string{
		"ana@example.com",
		"ana.b+bell@mail.example.co.uk",
		"ana_b@sub.example.org",
		"ana-b@example-town.com",
	}

	for _, email := range tests {
		t.Run(email, func(t *testing.T) {
			h := newInviteHarness(t)
			if _, err := h.svc.Create(context.Background(), h.inviter, email, ""); err != nil {
				t.Errorf("Create(%q) unexpected error: %v", email, err)
			}
		})
	}
}

func TestInviteService_Create_RejectsAnOverlongNote(t *testing.T) {
	h := newInviteHarness(t)

	_, err := h.svc.Create(context.Background(), h.inviter, "a@example.com", strings.Repeat("é", maxInviteNoteLength+1))
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Create() error = %v, want ErrValidation", err)
	}

	// Counted in runes, so a note of exactly the limit in a multi-byte script
	// is accepted even though it is well over the limit in bytes.
	if _, err := h.svc.Create(context.Background(), h.inviter, "b@example.com", strings.Repeat("é", maxInviteNoteLength)); err != nil {
		t.Errorf("Create() with a note at the rune limit: %v", err)
	}
}

// --- the combined daily budget ---

func TestInviteService_Create_InvitesAndVouchesShareOneDailyAllowance(t *testing.T) {
	tests := []struct {
		name           string
		invitesToday   int
		vouchesToday   int
		wantRefusal    bool
		refusalMessage string
	}{
		{name: "nothing spent", invitesToday: 0, vouchesToday: 0},
		{name: "two invites, none left over after this one", invitesToday: 2, vouchesToday: 0},
		{name: "two vouches", invitesToday: 0, vouchesToday: 2},
		{name: "one of each", invitesToday: 1, vouchesToday: 1},
		{name: "three invites", invitesToday: 3, vouchesToday: 0, wantRefusal: true},
		{name: "three vouches", invitesToday: 0, vouchesToday: 3, wantRefusal: true},
		{
			name: "two invites and a vouch is the combined limit",
			// The case that matters: neither count alone reaches three, and the
			// budget is still spent. A per-mechanism limit would let this
			// member put six endorsements into the world in a day.
			invitesToday: 2, vouchesToday: 1, wantRefusal: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newInviteHarness(t)
			for i := 0; i < tt.invitesToday; i++ {
				mustCreate(t, h, h.inviter, spentAddress(i), "")
			}
			for i := 0; i < tt.vouchesToday; i++ {
				h.vouches.seedVouch(h.inviter.ID, "vouchee-"+string(rune('a'+i)), fixedNow)
			}

			_, err := h.svc.Create(context.Background(), h.inviter, "fresh@example.com", "")
			if tt.wantRefusal {
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("Create() error = %v, want ErrValidation for the spent budget", err)
				}
				if !strings.Contains(err.Error(), "share one allowance") {
					t.Errorf("Create() error = %q, want it to explain the shared allowance", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Create() unexpected error: %v", err)
			}
		})
	}
}

func spentAddress(i int) string {
	return "spent" + string(rune('a'+i)) + "@example.com"
}

func TestInviteService_Create_CouncilIsExemptFromTheDailyAllowance(t *testing.T) {
	h := newInviteHarness(t)

	// Well past what any member could send. Bootstrapping an invite-only town
	// is exactly this: one council member inviting everybody they know, and a
	// three-a-day cap would make standing a town up take a fortnight.
	for i := 0; i < 10; i++ {
		if _, err := h.svc.Create(context.Background(), h.council, councilInviteAddress(i), ""); err != nil {
			t.Fatalf("council invite %d refused: %v", i, err)
		}
	}
}

func councilInviteAddress(i int) string {
	return "resident" + string(rune('a'+i)) + "@example.com"
}

func TestInviteService_Create_YesterdaysInvitesDoNotCount(t *testing.T) {
	h := newInviteHarness(t)
	yesterday := startOfDay(fixedNow).Add(-time.Hour)
	for i := 0; i < 3; i++ {
		h.invites.seed(&domain.Invite{
			ID: "old-" + string(rune('a'+i)), Email: spentAddress(i), InviterID: h.inviter.ID,
			CreatedAt: yesterday, ExpiresAt: yesterday.Add(InviteTTL),
		}, "")
	}

	if _, err := h.svc.Create(context.Background(), h.inviter, "fresh@example.com", ""); err != nil {
		t.Fatalf("Create() unexpected error: %v", err)
	}
}

// --- one live invitation per address ---

func TestInviteService_Create_RefusesASecondLiveInvitationForOneAddress(t *testing.T) {
	h := newInviteHarness(t)
	mustCreate(t, h, h.inviter, "newcomer@example.com", "")

	// A different inviter, and a different capitalisation: neither makes it a
	// different address.
	_, err := h.svc.Create(context.Background(), h.council, "NewComer@Example.com", "")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Create() error = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "already open") {
		t.Errorf("Create() error = %q, want it to say an invitation is already open", err)
	}
}

func TestInviteService_Create_ReapsAnExpiredInvitationSoTheAddressCanBeReused(t *testing.T) {
	h := newInviteHarness(t)
	expired := h.invites.seed(&domain.Invite{
		ID: "expired-1", Email: "newcomer@example.com", InviterID: h.inviter.ID,
		CreatedAt: fixedNow.Add(-30 * 24 * time.Hour),
		ExpiresAt: fixedNow.Add(-16 * 24 * time.Hour),
	}, "")

	creation := mustCreate(t, h, h.inviter, "newcomer@example.com", "")

	if creation.Invite.ID == expired.ID {
		t.Fatal("Create() reused the expired row rather than issuing a new invitation")
	}
	if len(h.invites.reaped) != 1 || h.invites.reaped[0] != expired.ID {
		t.Errorf("reaped = %v, want the expired invitation freed", h.invites.reaped)
	}

	// Reaping stamps revoked_at, but at a time already past expires_at, so the
	// inviter still sees the invitation as expired rather than as one they
	// withdrew.
	if got := expired.Status(fixedNow); got != domain.InviteExpired {
		t.Errorf("reaped invitation status = %q, want %q", got, domain.InviteExpired)
	}
}

func TestInviteService_Create_RefusesAnAddressThatAlreadyAcceptedOne(t *testing.T) {
	h := newInviteHarness(t)
	consumedAt := fixedNow.Add(-24 * time.Hour)
	h.invites.seed(&domain.Invite{
		ID: "done-1", Email: "newcomer@example.com", InviterID: h.inviter.ID,
		CreatedAt: fixedNow.Add(-48 * time.Hour), ExpiresAt: fixedNow.Add(InviteTTL),
		ConsumedAt: &consumedAt, ConsumedBy: "newcomer-1",
	}, "")

	_, err := h.svc.Create(context.Background(), h.inviter, "newcomer@example.com", "")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Create() error = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "already accepted") {
		t.Errorf("Create() error = %q, want it to say the address already accepted an invitation", err)
	}
}

func TestInviteService_Create_MapsTheUniqueIndexOntoAValidationError(t *testing.T) {
	h := newInviteHarness(t)
	// What losing the race looks like from inside: the liveness check passed
	// and the insert then hit the constraint.
	h.invites.createErr = ErrValidation

	_, err := h.svc.Create(context.Background(), h.inviter, "newcomer@example.com", "")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Create() error = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "already open") {
		t.Errorf("Create() error = %q, want the already-open message", err)
	}
}

// The budget is checked before anything is read about the address, so a member
// who is out of allowance cannot use refusals to discover who has been invited.
func TestInviteService_Create_ChecksTheBudgetBeforeLookingUpTheAddress(t *testing.T) {
	h := newInviteHarness(t)
	for i := 0; i < 3; i++ {
		mustCreate(t, h, h.inviter, spentAddress(i), "")
	}
	h.invites.blockingErr = errors.New("this lookup must not run")

	_, err := h.svc.Create(context.Background(), h.inviter, spentAddress(0), "")
	if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "share one allowance") {
		t.Fatalf("Create() error = %v, want the budget refusal and no address lookup", err)
	}
}

// --- invitation links ---

func TestInviteService_Create_ReturnsARelativeLinkWithoutAPublicURL(t *testing.T) {
	h := newInviteHarness(t)
	h.svc.SetPublicURL("")

	creation := mustCreate(t, h, h.inviter, "newcomer@example.com", "")

	if !strings.HasPrefix(creation.URL, "/auth/registration?invite=") {
		t.Errorf("invite_url = %q, want a site-relative path", creation.URL)
	}
}

func TestInviteService_Create_TrimsATrailingSlashFromThePublicURL(t *testing.T) {
	h := newInviteHarness(t)
	h.svc.SetPublicURL("https://bell.example.test/")

	creation := mustCreate(t, h, h.inviter, "newcomer@example.com", "")

	if strings.Contains(creation.URL, "test//auth") {
		t.Errorf("invite_url = %q, want no doubled slash", creation.URL)
	}
}

// --- mail ---

func TestInviteService_Create_SendsTheInvitation(t *testing.T) {
	h := newInviteHarness(t)
	h.config.config["town_name"] = "Bellville"

	creation := mustCreate(t, h, h.inviter, "newcomer@example.com", "we met at the market")

	if !creation.EmailSent {
		t.Fatalf("email_sent = false, want true (error %q)", creation.EmailError)
	}
	if creation.EmailError != "" {
		t.Errorf("email_error = %q, want empty on a successful send", creation.EmailError)
	}
	if len(h.mailer.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(h.mailer.sent))
	}

	msg := h.mailer.sent[0]
	if msg.To != "newcomer@example.com" {
		t.Errorf("To = %q", msg.To)
	}
	if want := "Ana invited you to join Bellville"; msg.Subject != want {
		t.Errorf("Subject = %q, want %q", msg.Subject, want)
	}
	for _, want := range []string{
		"we met at the market", // the note
		creation.URL,           // the link
		"14 days",              // the expiry
		"vouching for you",     // what accepting means
	} {
		if !strings.Contains(msg.Body, want) {
			t.Errorf("body does not mention %q:\n%s", want, msg.Body)
		}
	}
}

func TestInviteService_Create_SurvivesAFailedSend(t *testing.T) {
	h := newInviteHarness(t)
	h.mailer.err = errors.New("relay refused the connection")

	creation := mustCreate(t, h, h.inviter, "newcomer@example.com", "")

	// The invitation is the point; the email is how it usually travels. A
	// failed send must leave the member holding a working link.
	if creation.EmailSent {
		t.Error("email_sent = true after a failed send")
	}
	if creation.EmailError == "" {
		t.Error("email_error is empty after a failed send")
	}
	if creation.URL == "" {
		t.Error("invite_url is empty; the member has no way to pass the invitation on")
	}
	if _, ok := h.invites.invites[creation.Invite.ID]; !ok {
		t.Error("the invitation was not stored")
	}
}

func TestInviteService_Create_ExplainsWhenSendingIsNotConfigured(t *testing.T) {
	h := newInviteHarness(t)
	h.svc.SetMailer(nil)

	creation := mustCreate(t, h, h.inviter, "newcomer@example.com", "")

	if creation.EmailSent {
		t.Error("email_sent = true with no mailer configured")
	}
	if !strings.Contains(creation.EmailError, "not configured") {
		t.Errorf("email_error = %q, want it to say sending is not configured", creation.EmailError)
	}
}

func TestInviteService_Create_PrefersTheCouncilsTownName(t *testing.T) {
	h := newInviteHarness(t)
	h.config.config["town_name"] = "Little Bellville"

	mustCreate(t, h, h.inviter, "newcomer@example.com", "")

	if !strings.Contains(h.mailer.sent[0].Subject, "Little Bellville") {
		t.Errorf("Subject = %q, want the town_name from config", h.mailer.sent[0].Subject)
	}
}

func TestInviteService_Create_FallsBackToTheConfiguredTownName(t *testing.T) {
	h := newInviteHarness(t)
	// No town_name row at all, which is what a town that never set one looks
	// like.

	mustCreate(t, h, h.inviter, "newcomer@example.com", "")

	if !strings.Contains(h.mailer.sent[0].Subject, "Bellville") {
		t.Errorf("Subject = %q, want the TOWN_NAME fallback", h.mailer.sent[0].Subject)
	}
}

func TestInviteService_Create_NamesANamelessInviterSayably(t *testing.T) {
	h := newInviteHarness(t)
	h.inviter.DisplayName = ""

	mustCreate(t, h, h.inviter, "newcomer@example.com", "")

	if strings.HasPrefix(h.mailer.sent[0].Subject, " invited") {
		t.Errorf("Subject = %q, want a fallback name rather than a blank", h.mailer.sent[0].Subject)
	}
}

// --- List and Revoke ---

func TestInviteService_List_ReturnsTheCallersOwnInvitations(t *testing.T) {
	h := newInviteHarness(t)
	mustCreate(t, h, h.inviter, "mine@example.com", "")
	mustCreate(t, h, h.council, "theirs@example.com", "")

	invites, err := h.svc.List(context.Background(), h.inviter.ID)
	if err != nil {
		t.Fatalf("List() unexpected error: %v", err)
	}
	if len(invites) != 1 || invites[0].Email != "mine@example.com" {
		t.Fatalf("List() = %v, want only the caller's own invitation", invites)
	}
}

func TestInviteService_Revoke_WithdrawsAnOpenInvitation(t *testing.T) {
	h := newInviteHarness(t)
	creation := mustCreate(t, h, h.inviter, "newcomer@example.com", "")

	if err := h.svc.Revoke(context.Background(), creation.Invite.ID, h.inviter.ID); err != nil {
		t.Fatalf("Revoke() unexpected error: %v", err)
	}

	stored := h.invites.invites[creation.Invite.ID]
	if got := stored.Status(fixedNow); got != domain.InviteRevoked {
		t.Errorf("status after revoke = %q, want %q", got, domain.InviteRevoked)
	}
	// And the address is free again, which is the point of withdrawing.
	if _, err := h.svc.Create(context.Background(), h.inviter, "newcomer@example.com", ""); err != nil {
		t.Errorf("re-inviting a revoked address: %v", err)
	}
}

func TestInviteService_Revoke_RefusesAnythingButTheCallersOwnOpenInvitation(t *testing.T) {
	consumedAt := fixedNow.Add(-time.Hour)
	tests := []struct {
		name   string
		invite *domain.Invite
		caller string
	}{
		{
			name:   "somebody else's",
			invite: &domain.Invite{ID: "i1", Email: "a@example.com", InviterID: "council-1", CreatedAt: fixedNow, ExpiresAt: fixedNow.Add(InviteTTL)},
			caller: "inviter-1",
		},
		{
			name: "already accepted",
			invite: &domain.Invite{ID: "i2", Email: "b@example.com", InviterID: "inviter-1", CreatedAt: fixedNow,
				ExpiresAt: fixedNow.Add(InviteTTL), ConsumedAt: &consumedAt, ConsumedBy: "newcomer-1"},
			caller: "inviter-1",
		},
		{
			name: "already expired",
			invite: &domain.Invite{ID: "i3", Email: "c@example.com", InviterID: "inviter-1",
				CreatedAt: fixedNow.Add(-30 * 24 * time.Hour), ExpiresAt: fixedNow.Add(-time.Hour)},
			caller: "inviter-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newInviteHarness(t)
			h.invites.seed(tt.invite, "")

			err := h.svc.Revoke(context.Background(), tt.invite.ID, tt.caller)
			// One answer for all three, so nothing about somebody else's
			// invitation can be inferred from the refusal.
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("Revoke() error = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestInviteService_Revoke_RefusesAnUnknownID(t *testing.T) {
	h := newInviteHarness(t)

	if err := h.svc.Revoke(context.Background(), "no-such-invite", h.inviter.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Revoke() error = %v, want ErrNotFound", err)
	}
}

// --- Lookup ---

func TestInviteService_Lookup_GreetsTheInvitee(t *testing.T) {
	h := newInviteHarness(t)
	h.config.config["town_name"] = "Bellville"
	creation := mustCreate(t, h, h.inviter, "newcomer@example.com", "")
	// The inviter's name is joined in by the query; the fake stores what was
	// written, so it is supplied here the way the repository would.
	h.invites.invites[creation.Invite.ID].InviterDisplayName = "Ana"
	token := rawTokenFrom(t, creation.URL)

	lookup, err := h.svc.Lookup(context.Background(), token)
	if err != nil {
		t.Fatalf("Lookup() unexpected error: %v", err)
	}
	if lookup.Email != "newcomer@example.com" {
		t.Errorf("email = %q", lookup.Email)
	}
	if lookup.TownName != "Bellville" {
		t.Errorf("town_name = %q", lookup.TownName)
	}
	if lookup.InviterDisplayName != "Ana" {
		t.Errorf("inviter_display_name = %q", lookup.InviterDisplayName)
	}
}

// Every dead end is ErrNotFound, and that uniformity is the security property:
// a caller working through guessed tokens must not be able to tell a token that
// was once real from one that never was.
func TestInviteService_Lookup_IsUniformlyNotFound(t *testing.T) {
	consumedAt := fixedNow.Add(-time.Hour)
	revokedAt := fixedNow.Add(-time.Hour)

	tests := []struct {
		name   string
		invite *domain.Invite
		token  string
	}{
		{name: "unknown token", token: "never-existed"},
		{name: "empty token", token: ""},
		{name: "whitespace token", token: "   "},
		{
			name: "consumed",
			invite: &domain.Invite{ID: "i1", Email: "a@example.com", InviterID: "inviter-1", CreatedAt: fixedNow,
				ExpiresAt: fixedNow.Add(InviteTTL), ConsumedAt: &consumedAt, ConsumedBy: "newcomer-1"},
		},
		{
			name: "revoked",
			invite: &domain.Invite{ID: "i2", Email: "b@example.com", InviterID: "inviter-1", CreatedAt: fixedNow,
				ExpiresAt: fixedNow.Add(InviteTTL), RevokedAt: &revokedAt},
		},
		{
			name: "expired",
			invite: &domain.Invite{ID: "i3", Email: "c@example.com", InviterID: "inviter-1",
				CreatedAt: fixedNow.Add(-30 * 24 * time.Hour), ExpiresAt: fixedNow.Add(-time.Hour)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newInviteHarness(t)
			token := tt.token
			if tt.invite != nil {
				token = "token-for-" + tt.invite.ID
				h.invites.seed(tt.invite, hashInviteToken(token))
			}

			_, err := h.svc.Lookup(context.Background(), token)
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("Lookup() error = %v, want ErrNotFound", err)
			}
		})
	}
}

// --- registration mode ---

func TestInviteService_InviteRequired(t *testing.T) {
	tests := []struct {
		name  string
		value string
		set   bool
		want  bool
	}{
		{name: "invite mode", value: "invite", set: true, want: true},
		{name: "open mode", value: "open", set: true, want: false},
		{name: "open, capitalised by hand in the database", value: "Open", set: true, want: false},
		{name: "no row at all defaults to invitations", set: false, want: true},
		{name: "an unrecognised value is treated as invitations", value: "banana", set: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newInviteHarness(t)
			if tt.set {
				h.config.config[registrationModeKey] = tt.value
			}

			got, err := h.svc.InviteRequired(context.Background())
			if err != nil {
				t.Fatalf("InviteRequired() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("InviteRequired() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- Redeem ---

func TestInviteService_Redeem_ConsumesTheInvitationAndVouches(t *testing.T) {
	h := newInviteHarness(t)
	creation := mustCreate(t, h, h.inviter, "newcomer@example.com", "")

	// Capitalised the way a mail client might present it: matching is
	// case-insensitive, so the invitation still finds its invitee.
	if err := h.svc.Redeem(context.Background(), h.newcomer, "NewComer@Example.com"); err != nil {
		t.Fatalf("Redeem() unexpected error: %v", err)
	}

	stored := h.invites.invites[creation.Invite.ID]
	if stored.ConsumedAt == nil || stored.ConsumedBy != h.newcomer.ID {
		t.Fatalf("invitation not consumed by the newcomer: %+v", stored)
	}
	if got, want := stored.Status(fixedNow), domain.InviteAccepted; got != want {
		t.Errorf("status = %q, want %q", got, want)
	}
	if len(h.voucher.calls) != 1 || h.voucher.calls[0] != [2]string{h.inviter.ID, h.newcomer.ID} {
		t.Fatalf("vouch calls = %v, want one from the inviter to the newcomer", h.voucher.calls)
	}
	// The caller's copy of the user is raised too, so the request that redeemed
	// the invitation is answered as the member it just made.
	if h.newcomer.Role != domain.RoleMember {
		t.Errorf("newcomer role = %q, want %q", h.newcomer.Role, domain.RoleMember)
	}
}

func TestInviteService_Redeem_IsIdempotent(t *testing.T) {
	h := newInviteHarness(t)
	mustCreate(t, h, h.inviter, "newcomer@example.com", "")

	if err := h.svc.Redeem(context.Background(), h.newcomer, "newcomer@example.com"); err != nil {
		t.Fatalf("first Redeem(): %v", err)
	}
	// The role is raised by the first redemption, so a second call from the
	// middleware short-circuits — but force the pending case too, which is what
	// a redemption that failed to promote would leave behind.
	h.newcomer.Role = domain.RolePending
	if err := h.svc.Redeem(context.Background(), h.newcomer, "newcomer@example.com"); err != nil {
		t.Fatalf("second Redeem(): %v", err)
	}

	if len(h.invites.consumed) != 1 {
		t.Errorf("consumed %d times, want exactly 1", len(h.invites.consumed))
	}
	if len(h.voucher.calls) != 1 {
		t.Errorf("vouched %d times, want exactly 1", len(h.voucher.calls))
	}
}

func TestInviteService_Redeem_DoesNothingWithoutAWaitingInvitation(t *testing.T) {
	tests := []struct {
		name  string
		user  *domain.User
		email string
	}{
		{name: "no invitation for this address", user: &domain.User{ID: "newcomer-1", Role: domain.RolePending}, email: "stranger@example.com"},
		{name: "no address at all", user: &domain.User{ID: "newcomer-1", Role: domain.RolePending}, email: ""},
		{name: "nil user", user: nil, email: "newcomer@example.com"},
		{name: "already a member", user: &domain.User{ID: "m", Role: domain.RoleMember}, email: "newcomer@example.com"},
		{name: "banned", user: &domain.User{ID: "b", Role: domain.RoleBanned}, email: "newcomer@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newInviteHarness(t)
			mustCreate(t, h, h.inviter, "newcomer@example.com", "")

			if err := h.svc.Redeem(context.Background(), tt.user, tt.email); err != nil {
				t.Fatalf("Redeem() unexpected error: %v", err)
			}
			if len(h.voucher.calls) != 0 {
				t.Errorf("vouched %d times, want none", len(h.voucher.calls))
			}
			if len(h.invites.consumed) != 0 {
				t.Errorf("consumed %d invitations, want none", len(h.invites.consumed))
			}
		})
	}
}

func TestInviteService_Redeem_ConsumesButDoesNotVouchWhenTheInviterHasCollapsed(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*domain.User)
	}{
		{"trust has fallen below the threshold", func(u *domain.User) { u.TrustScore = 40 }},
		{"suspended", func(u *domain.User) { u.IsActive = false }},
		{"banned", func(u *domain.User) { u.Role = domain.RoleBanned }},
		{"demoted back to pending", func(u *domain.User) { u.Role = domain.RolePending }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newInviteHarness(t)
			creation := mustCreate(t, h, h.inviter, "newcomer@example.com", "")
			tt.mutate(h.inviter)

			// Not an error: the invitation was honoured, it simply turned out
			// to be worth nothing. Failing here would make the newcomer's every
			// subsequent request log an error about a state that will never
			// improve.
			if err := h.svc.Redeem(context.Background(), h.newcomer, "newcomer@example.com"); err != nil {
				t.Fatalf("Redeem() error = %v, want nil", err)
			}

			if h.invites.invites[creation.Invite.ID].ConsumedAt == nil {
				t.Error("the invitation was not consumed")
			}
			if len(h.voucher.calls) != 0 {
				t.Errorf("vouched %d times, want none from a collapsed inviter", len(h.voucher.calls))
			}
			if h.newcomer.Role != domain.RolePending {
				t.Errorf("newcomer role = %q, want them left pending", h.newcomer.Role)
			}
		})
	}
}

func TestInviteService_Redeem_ReportsAFailedVouch(t *testing.T) {
	h := newInviteHarness(t)
	mustCreate(t, h, h.inviter, "newcomer@example.com", "")
	h.voucher.err = errors.New("graph unavailable")

	err := h.svc.Redeem(context.Background(), h.newcomer, "newcomer@example.com")
	if err == nil {
		t.Fatal("Redeem() error = nil, want the vouch failure reported for the log")
	}
	if h.newcomer.Role != domain.RolePending {
		t.Errorf("newcomer role = %q, want them left pending", h.newcomer.Role)
	}
}

func TestInviteService_Redeem_TreatsALostConsumeRaceAsDone(t *testing.T) {
	h := newInviteHarness(t)
	mustCreate(t, h, h.inviter, "newcomer@example.com", "")
	// What the loser of the race sees: the row was live at the read and the
	// conditional UPDATE then matched nothing.
	h.invites.consumeErr = ErrNotFound

	if err := h.svc.Redeem(context.Background(), h.newcomer, "newcomer@example.com"); err != nil {
		t.Fatalf("Redeem() error = %v, want nil", err)
	}
	if len(h.voucher.calls) != 0 {
		t.Errorf("vouched %d times, want none: the other request owns this invitation", len(h.voucher.calls))
	}
}

func TestInviteService_Redeem_IgnoresAnExpiredInvitation(t *testing.T) {
	h := newInviteHarness(t)
	h.invites.seed(&domain.Invite{
		ID: "expired-1", Email: "newcomer@example.com", InviterID: h.inviter.ID,
		CreatedAt: fixedNow.Add(-30 * 24 * time.Hour), ExpiresAt: fixedNow.Add(-time.Hour),
	}, "")

	if err := h.svc.Redeem(context.Background(), h.newcomer, "newcomer@example.com"); err != nil {
		t.Fatalf("Redeem() error = %v, want nil", err)
	}
	if len(h.voucher.calls) != 0 {
		t.Errorf("vouched %d times on an expired invitation, want none", len(h.voucher.calls))
	}
}

// --- tokens ---

func TestNewInviteToken_IsUnpredictableAndURLSafe(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		raw, hash, err := newInviteToken()
		if err != nil {
			t.Fatalf("newInviteToken(): %v", err)
		}
		if seen[raw] {
			t.Fatal("newInviteToken() produced a duplicate")
		}
		seen[raw] = true

		if strings.ContainsAny(raw, "+/=&?#") {
			t.Errorf("token %q contains characters that need escaping in a URL", raw)
		}
		if hash == raw {
			t.Error("the stored hash equals the raw token")
		}
		if len(hash) != 64 {
			t.Errorf("hash length = %d, want 64 hex characters", len(hash))
		}
	}
}

func rawTokenFrom(t *testing.T, inviteURL string) string {
	t.Helper()
	_, token, found := strings.Cut(inviteURL, "invite=")
	if !found {
		t.Fatalf("no token in %q", inviteURL)
	}
	return token
}

// --- the allowance is shared in both directions ---

// InviteService.Create counting vouches is only half the rule. Without the
// mirror image in VouchService a member could spend the allowance twice by
// alternating: three invitations, refused a fourth, then three vouches — six
// endorsements from a rule that says three.
func TestVouchService_Vouch_CountsInvitesAgainstTheSameDailyAllowance(t *testing.T) {
	tests := []struct {
		name         string
		invitesToday int
		vouchesToday int
		wantRefusal  bool
	}{
		{name: "nothing spent", invitesToday: 0, vouchesToday: 0},
		{name: "two invitations leave one endorsement", invitesToday: 2, vouchesToday: 0},
		{name: "three invitations spend the day", invitesToday: 3, vouchesToday: 0, wantRefusal: true},
		{name: "two invitations and a vouch spend it", invitesToday: 2, vouchesToday: 1, wantRefusal: true},
		{name: "three vouches, as before invitations existed", invitesToday: 0, vouchesToday: 3, wantRefusal: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newInviteHarness(t)
			users := newMockUserGetter()
			users.users[h.inviter.ID] = h.inviter
			users.users["vouchee-new"] = pendingUser("vouchee-new")

			for i := 0; i < tt.invitesToday; i++ {
				mustCreate(t, h, h.inviter, spentAddress(i), "")
			}
			for i := 0; i < tt.vouchesToday; i++ {
				h.vouches.seedVouch(h.inviter.ID, "vouchee-"+string(rune('a'+i)), fixedNow)
			}

			svc := NewVouchService(h.vouches, newMockGraph(), users, fixedClock)
			svc.SetInviteCounter(h.invites)

			_, err := svc.Vouch(context.Background(), h.inviter.ID, "vouchee-new")
			if tt.wantRefusal {
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("Vouch() error = %v, want ErrValidation for the spent allowance", err)
				}
				if !strings.Contains(err.Error(), "share one allowance") {
					t.Errorf("Vouch() error = %q, want it to explain the shared allowance", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Vouch() unexpected error: %v", err)
			}
		})
	}
}

// Without a counter attached the limit counts vouches alone, which is what
// every deployment did before invitations existed.
func TestVouchService_Vouch_WithoutAnInviteCounterCountsVouchesAlone(t *testing.T) {
	h := newInviteHarness(t)
	users := newMockUserGetter()
	users.users[h.inviter.ID] = h.inviter
	users.users["vouchee-new"] = pendingUser("vouchee-new")
	for i := 0; i < 3; i++ {
		mustCreate(t, h, h.inviter, spentAddress(i), "")
	}

	svc := NewVouchService(h.vouches, newMockGraph(), users, fixedClock)

	if _, err := svc.Vouch(context.Background(), h.inviter.ID, "vouchee-new"); err != nil {
		t.Fatalf("Vouch() unexpected error: %v", err)
	}
}

// The exemption has to hold on both sides too, or a council member who spent
// the morning inviting people would find themselves unable to vouch.
func TestVouchService_Vouch_CouncilIsExemptFromTheDailyAllowance(t *testing.T) {
	h := newInviteHarness(t)
	users := newMockUserGetter()
	users.users[h.council.ID] = h.council

	for i := 0; i < 5; i++ {
		mustCreate(t, h, h.council, councilInviteAddress(i), "")
	}

	svc := NewVouchService(h.vouches, newMockGraph(), users, fixedClock)
	svc.SetInviteCounter(h.invites)

	for i := 0; i < 5; i++ {
		id := "vouchee-council-" + string(rune('a'+i))
		users.users[id] = pendingUser(id)
		if _, err := svc.Vouch(context.Background(), h.council.ID, id); err != nil {
			t.Fatalf("council vouch %d refused: %v", i, err)
		}
	}
}

package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/mail"
)

const (
	// InviteTTL is how long an invitation stays redeemable.
	//
	// Two weeks is long enough to survive a holiday and an ignored inbox, and
	// short enough that a link forwarded into a mailing list archive stops
	// working before anybody stumbles on it. It also bounds how stale the
	// endorsement can be: the inviter's standing is re-checked at redemption,
	// but the decision to invite was made on the day it was sent.
	InviteTTL = 14 * 24 * time.Hour

	// maxInviteNoteLength bounds the personal note, in runes. Five hundred is
	// the bio limit — a note is the same kind of thing, a paragraph a person
	// writes about another person — and runes rather than bytes so a note in a
	// non-Latin script gets the same room.
	maxInviteNoteLength = 500

	// maxEmailLength is the practical ceiling on an address: 64 octets of local
	// part plus 255 of domain, per RFC 5321. It is a sanity bound, not a
	// validator.
	maxEmailLength = 320

	// inviteTokenBytes is the entropy behind an invitation link. Thirty-two
	// bytes from crypto/rand is the same budget as a session token, which is
	// what this is: whoever holds it can get through the registration gate.
	inviteTokenBytes = 32

	// registrationModeKey and its two values live in town_config, council-
	// writable through the config endpoint.
	registrationModeKey = "registration_mode"
	// RegistrationModeInvite requires a live invitation to register;
	// RegistrationModeOpen is the original behaviour, where anybody may sign up
	// and wait to be vouched for or approved.
	RegistrationModeInvite = "invite"
	RegistrationModeOpen   = "open"
)

// InviteRepository is the persistence an InviteService needs.
//
// The raw token appears nowhere in it. CreateInvite takes a hash the service
// has already computed and the lookups take a hash too, so the only place the
// raw value exists is in the response to the request that created it and in the
// invitee's inbox.
type InviteRepository interface {
	CreateInvite(ctx context.Context, invite *domain.Invite, tokenHash string) error
	GetLiveInviteByTokenHash(ctx context.Context, tokenHash string, now time.Time) (*domain.Invite, error)
	GetLiveInviteByEmail(ctx context.Context, email string, now time.Time) (*domain.Invite, error)
	// GetBlockingInviteByEmail returns the unconsumed, unrevoked row holding an
	// address whether or not it has expired — see Create for why expiry has to
	// be judged here rather than in the unique index.
	GetBlockingInviteByEmail(ctx context.Context, email string) (*domain.Invite, error)
	CountConsumedInvitesByEmail(ctx context.Context, email string) (int64, error)
	CountInvitesByInviterSince(ctx context.Context, inviterID string, since time.Time) (int64, error)
	ListInvitesByInviter(ctx context.Context, inviterID string) ([]*domain.Invite, error)
	RevokeInvite(ctx context.Context, id, inviterID string, now time.Time) error
	ReapInvite(ctx context.Context, id string, now time.Time) error
	ConsumeInvite(ctx context.Context, id, userID string, now time.Time) (*domain.Invite, error)
}

// InviteVoucher is the vouch path an accepted invitation lands on.
//
// It is one method wide so that the invite service depends on the redemption
// path and nothing else — it must not be able to reach VouchService.Revoke or
// the ordinary Vouch that would charge the budget a second time.
type InviteVoucher interface {
	VouchFromInvite(ctx context.Context, voucherID, voucheeID string) (*domain.Vouch, error)
}

// InviteVouchCounter is the other half of the combined daily budget: how many
// vouches the member has already given today.
type InviteVouchCounter interface {
	CountVouchesByVoucherSince(ctx context.Context, voucherID string, since time.Time) (int64, error)
}

// InviteUserLookup reads the inviter back at redemption time.
type InviteUserLookup interface {
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
}

// InviteCreation is what a member gets back for creating an invitation: the
// invitation itself, the link to pass on, and an honest account of whether the
// email went out.
//
// EmailError is populated exactly when EmailSent is false and sending was
// attempted or was impossible. It is the reason the member has to be shown —
// an invitation whose mail bounced is still a usable invitation, provided they
// know to send the link themselves.
type InviteCreation struct {
	Invite     *domain.Invite
	URL        string
	EmailSent  bool
	EmailError string
}

// InviteLookup is the greeting a registration page shows somebody arriving on
// an invitation link. It carries no token, no ids and nothing about the town's
// membership: just enough to tell the invitee they are expected.
type InviteLookup struct {
	Email              string
	TownName           string
	InviterDisplayName string
}

// InviteService runs invitations: creating them, listing and withdrawing them,
// and turning an accepted one into the vouch it always was.
type InviteService struct {
	invites     InviteRepository
	vouchCounts InviteVouchCounter
	voucher     InviteVoucher
	users       InviteUserLookup
	config      ConfigRepository
	mailer      mail.Sender
	logger      *slog.Logger
	now         func() time.Time

	// publicURL is the base the invitation link is built on. Empty is
	// supported and means the link is returned as a site-relative path for the
	// client to absolutize — see inviteURL.
	publicURL string
	// townName is the fallback used when town_config carries no town_name,
	// which is the TOWN_NAME the process was started with.
	townName string
}

func NewInviteService(
	invites InviteRepository,
	vouchCounts InviteVouchCounter,
	voucher InviteVoucher,
	users InviteUserLookup,
	config ConfigRepository,
	logger *slog.Logger,
	clock func() time.Time,
) *InviteService {
	if clock == nil {
		clock = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &InviteService{
		invites:     invites,
		vouchCounts: vouchCounts,
		voucher:     voucher,
		users:       users,
		config:      config,
		logger:      logger,
		now:         clock,
	}
}

// SetMailer attaches the relay invitation mail goes out through. Left unset,
// invitations are still created and still work — the member is told the mail
// was not sent and hands the link over themselves, which is the documented
// behaviour on a deployment with no SMTP.
func (s *InviteService) SetMailer(m mail.Sender) { s.mailer = m }

// SetPublicURL sets the origin invitation links are built against, from
// PUBLIC_URL. Empty yields relative links; see inviteURL.
func (s *InviteService) SetPublicURL(u string) {
	s.publicURL = strings.TrimRight(strings.TrimSpace(u), "/")
}

// SetTownName sets the fallback town name used in invitation mail and in the
// public lookup when town_config has none.
func (s *InviteService) SetTownName(name string) { s.townName = strings.TrimSpace(name) }

// Create issues an invitation, and sends it.
//
// Every rule an invitation is subject to is applied here, in this order, and
// the order is deliberate:
//
//  1. The vouch rule. An invitation is a vouch in escrow, so the inviter must
//     be somebody who could vouch right now — CanVouch, the same test the
//     vouch endpoint applies. The route's member guard is not enough: it lets
//     through a member below the trust threshold.
//  2. Shape. A trimmed, lowercased, syntactically plausible address and a note
//     within the length bound. Both are the caller's own input and both are
//     told back to them.
//  3. Budget, before anything is looked up about the address. Invitations and
//     vouches share one daily allowance of three, because they are the same
//     act with different timing; council is exempt, mirroring the deliberate
//     decision to leave council approvals unlimited (see the /v1/vouches
//     route comments) — in an invite-only town the council's invitations are
//     how a town is populated in the first place. Checking the budget before
//     the address lookups also means a member cannot spend a refused request
//     probing whether an address has been invited: out of budget, they learn
//     nothing.
//  4. Whether the address is already spoken for.
//
// On the duplicate-account question the honest answer is that this service does
// not know. Kratos owns the identity table and cannot be queried by address
// cheaply, and asking it would turn this endpoint into an account-existence
// oracle for any member. So the check is app-side and narrow: an address that
// has already ACCEPTED an invitation is refused, because that person is already
// in the town by this door. Anything else is left to Kratos, which refuses a
// duplicate registration at sign-up. The cost is that inviting an existing
// resident who never used an invitation succeeds and produces a link that will
// not create an account; the invitation simply expires.
func (s *InviteService) Create(ctx context.Context, inviter *domain.User, email, note string) (*InviteCreation, error) {
	if inviter == nil {
		return nil, fmt.Errorf("%w: no inviter", ErrForbidden)
	}
	if !inviter.CanVouch() {
		return nil, fmt.Errorf("%w: inviter does not meet trust requirements", ErrForbidden)
	}

	email = normalizeEmail(email)
	if err := validateInviteEmail(email); err != nil {
		return nil, err
	}
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > maxInviteNoteLength {
		return nil, fmt.Errorf("%w: note exceeds %d characters", ErrValidation, maxInviteNoteLength)
	}

	now := s.now()
	if err := s.checkBudget(ctx, inviter, now); err != nil {
		return nil, err
	}
	if err := s.checkAddressFree(ctx, email, now); err != nil {
		return nil, err
	}

	rawToken, tokenHash, err := newInviteToken()
	if err != nil {
		return nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generating invite id: %w", err)
	}

	invite := &domain.Invite{
		ID:        id.String(),
		Email:     email,
		Note:      note,
		InviterID: inviter.ID,
		CreatedAt: now,
		ExpiresAt: now.Add(InviteTTL),
	}
	if err := s.invites.CreateInvite(ctx, invite, tokenHash); err != nil {
		if errors.Is(err, ErrValidation) {
			// The unique index fired: another request took this address between
			// checkAddressFree and here.
			return nil, fmt.Errorf("%w: an invitation for that address is already open", ErrValidation)
		}
		return nil, fmt.Errorf("creating invite: %w", err)
	}

	creation := &InviteCreation{Invite: invite, URL: s.inviteURL(rawToken)}
	s.deliver(ctx, creation, inviter)
	return creation, nil
}

// checkBudget applies the combined daily allowance, or exempts the council.
func (s *InviteService) checkBudget(ctx context.Context, inviter *domain.User, now time.Time) error {
	if inviter.IsCouncil() {
		return nil
	}

	since := startOfDay(now)
	invitesToday, err := s.invites.CountInvitesByInviterSince(ctx, inviter.ID, since)
	if err != nil {
		return fmt.Errorf("counting today's invites: %w", err)
	}
	vouchesToday, err := s.vouchCounts.CountVouchesByVoucherSince(ctx, inviter.ID, since)
	if err != nil {
		return fmt.Errorf("counting today's vouches: %w", err)
	}
	if invitesToday+vouchesToday >= dailyVouchLimit {
		return fmt.Errorf("%w: daily limit (%d) reached; invites and vouches share one allowance",
			ErrValidation, dailyVouchLimit)
	}
	return nil
}

// checkAddressFree refuses an address that is already invited or already in,
// and clears an expired invitation out of the way.
//
// The reap is what makes expiry meaningful. idx_invites_live_email cannot test
// expiry — an index predicate must be immutable, so now() is unavailable to it —
// so an expired invitation still occupies its address as far as the constraint
// is concerned, and without this a mistyped address would be unusable forever.
// Revoking rather than deleting keeps the history, and stamping revoked_at at a
// time already past expires_at means the row still reads as "expired" to the
// inviter rather than as something they withdrew (see domain.Invite.Status).
//
// A failed reap is not fatal here: the insert that follows will lose to the
// unique index and the caller is told the address is taken, which is the same
// answer with less explanation.
func (s *InviteService) checkAddressFree(ctx context.Context, email string, now time.Time) error {
	accepted, err := s.invites.CountConsumedInvitesByEmail(ctx, email)
	if err != nil {
		return fmt.Errorf("checking accepted invites: %w", err)
	}
	if accepted > 0 {
		return fmt.Errorf("%w: that address has already accepted an invitation", ErrValidation)
	}

	blocking, err := s.invites.GetBlockingInviteByEmail(ctx, email)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("checking existing invites: %w", err)
	}
	if blocking.IsLive(now) {
		return fmt.Errorf("%w: an invitation for that address is already open", ErrValidation)
	}

	if err := s.invites.ReapInvite(ctx, blocking.ID, now); err != nil {
		s.logger.Warn("reaping expired invite failed; the insert will refuse the address instead",
			"invite_id", blocking.ID, "error", err)
	}
	return nil
}

// deliver sends the invitation mail and records the outcome on the creation.
//
// It never returns an error, because an invitation that could not be emailed is
// still an invitation: the link is in the response, and a member who is told
// the mail failed can pass it on by hand. Hiding the failure would be the worse
// half of both options — the member would believe an email is on its way.
func (s *InviteService) deliver(ctx context.Context, creation *InviteCreation, inviter *domain.User) {
	if s.mailer == nil {
		creation.EmailError = "email sending is not configured on this deployment; send the invitation link yourself"
		return
	}

	townName := s.resolveTownName(ctx)
	msg := mail.Message{
		To:      creation.Invite.Email,
		Subject: fmt.Sprintf("%s invited you to join %s", inviterName(inviter), townName),
		Body:    inviteEmailBody(inviterName(inviter), townName, creation.Invite.Note, creation.URL),
	}
	if err := s.mailer.Send(ctx, msg); err != nil {
		s.logger.Warn("sending invitation email failed",
			"invite_id", creation.Invite.ID, "error", err)
		creation.EmailError = "the invitation could not be emailed; send the link yourself"
		return
	}
	creation.EmailSent = true
}

// List returns the caller's own invitations, newest first.
func (s *InviteService) List(ctx context.Context, inviterID string) ([]*domain.Invite, error) {
	invites, err := s.invites.ListInvitesByInviter(ctx, inviterID)
	if err != nil {
		return nil, fmt.Errorf("listing invites: %w", err)
	}
	return invites, nil
}

// Revoke withdraws one of the caller's own open invitations.
//
// Ownership and openness are both enforced by the UPDATE's WHERE clause, so
// somebody else's invitation, an already-accepted one and an id that names
// nothing are one answer: ErrNotFound. Distinguishing them would tell a member
// which invitation ids exist.
func (s *InviteService) Revoke(ctx context.Context, id, inviterID string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: invite id is required", ErrValidation)
	}
	if err := s.invites.RevokeInvite(ctx, id, inviterID, s.now()); err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("revoking invite: %w", err)
	}
	return nil
}

// Lookup answers the public greeting for an invitation link.
//
// Every failure is ErrNotFound — unknown, consumed, revoked, expired, and an
// empty token alike — because this endpoint is unauthenticated and anything
// finer grained is a probe: "revoked" and "expired" would each confirm that a
// token was once real, and telling those apart from "never existed" turns the
// endpoint into a token oracle.
func (s *InviteService) Lookup(ctx context.Context, rawToken string) (*InviteLookup, error) {
	invite, err := s.LiveInviteByToken(ctx, rawToken)
	if err != nil {
		return nil, err
	}
	return &InviteLookup{
		Email:              invite.Email,
		TownName:           s.resolveTownName(ctx),
		InviterDisplayName: invite.InviterDisplayName,
	}, nil
}

// LiveInviteByToken resolves a raw token to a redeemable invitation, or
// ErrNotFound. It is what the registration gate calls, and Lookup's first step.
func (s *InviteService) LiveInviteByToken(ctx context.Context, rawToken string) (*domain.Invite, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, ErrNotFound
	}
	invite, err := s.invites.GetLiveInviteByTokenHash(ctx, hashInviteToken(rawToken), s.now())
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("looking up invite: %w", err)
	}
	return invite, nil
}

// InviteRequired reports whether this town admits new residents by invitation
// only.
//
// A town_config with no registration_mode row is treated as invite-only, which
// is the same default migration 00023 seeds. That is the fail-safe direction:
// the failure it prevents is a town that chose invitations quietly accepting
// strangers because a row went missing, and the failure it risks is a town
// having to set 'open' explicitly, which is a thing somebody notices and can
// fix. An unreadable config is an error rather than a default — see the
// registration gate, which refuses registration outright rather than guessing.
func (s *InviteService) InviteRequired(ctx context.Context) (bool, error) {
	mode, err := s.config.GetTownConfig(ctx, registrationModeKey)
	if errors.Is(err, ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading registration mode: %w", err)
	}
	return !strings.EqualFold(strings.TrimSpace(mode), RegistrationModeOpen), nil
}

// Redeem turns the invitation waiting for this address into the vouch it always
// was, and is a no-op when there is nothing waiting.
//
// It is called from the auth middleware for any pending user, not only for one
// that was just created. Redemption touches three things — the invitation, the
// vouch, the role — and is not one transaction, so a first attempt that failed
// partway must be able to complete later; running it on every request a pending
// user makes is what makes that self-healing rather than a support ticket. Once
// the vouch lands the user is a member and this is never called for them again.
//
// The daily vouch limit is bypassed (VouchFromInvite) because the budget was
// charged when the invitation was created. See that method for why charging
// again would be charging twice, on a day the inviter did not choose.
//
// The order is consume-then-vouch, and it matters. The conditional UPDATE
// behind ConsumeInvite is the exactly-once guard: two concurrent sign-ins both
// find the invitation live, and only one of them updates a row. The cost is
// that a vouch which fails after consumption leaves the invitation spent and
// the newcomer pending — no worse a position than an uninvited applicant, with
// the ordinary vouch and approval paths still open to them, and it is logged
// loudly. The reverse order would trade that for the possibility of two vouches
// from one invitation.
func (s *InviteService) Redeem(ctx context.Context, user *domain.User, email string) error {
	if user == nil || user.Role != domain.RolePending {
		return nil
	}
	email = normalizeEmail(email)
	if email == "" {
		return nil
	}

	now := s.now()
	invite, err := s.invites.GetLiveInviteByEmail(ctx, email, now)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("looking up invite for %s: %w", email, err)
	}

	if _, err := s.invites.ConsumeInvite(ctx, invite.ID, user.ID, now); err != nil {
		if errors.Is(err, ErrNotFound) {
			// Somebody else consumed it between the read and the update. The
			// invitation has been honoured; there is nothing left to do.
			return nil
		}
		return fmt.Errorf("consuming invite %s: %w", invite.ID, err)
	}

	inviter, err := s.users.GetUserByID(ctx, invite.InviterID)
	if err != nil {
		return fmt.Errorf("looking up inviter %s: %w", invite.InviterID, err)
	}
	if !inviter.CanVouch() {
		// The invitation is spent and no vouch lands. This is the deliberate
		// outcome, not a failure to retry: the inviter's standing is what the
		// invitation was worth, and if they have since been suspended, banned
		// or fallen below the trust threshold, that endorsement is no longer
		// theirs to give. The newcomer stays pending and can still be vouched
		// for by somebody else or approved by the council.
		s.logger.Warn("invitation consumed but the inviter can no longer vouch; newcomer stays pending",
			"invite_id", invite.ID, "inviter_id", inviter.ID, "user_id", user.ID,
			"inviter_role", inviter.Role, "inviter_trust", inviter.TrustScore,
			"inviter_active", inviter.IsActive)
		return nil
	}

	if _, err := s.voucher.VouchFromInvite(ctx, inviter.ID, user.ID); err != nil {
		return fmt.Errorf("vouching for invitee %s from invite %s: %w", user.ID, invite.ID, err)
	}

	// The vouch promoted them in the database; this brings the caller's copy
	// into line. Without it the request that redeemed the invitation would
	// still be answered as a pending user, so the invitee's first page load
	// after signing up would show the "waiting to be vouched for" screen for a
	// membership they already have. The user is loaded fresh per request, so
	// nothing else is holding this pointer.
	user.Role = domain.RoleMember

	s.logger.Info("invitation redeemed",
		"invite_id", invite.ID, "inviter_id", inviter.ID, "user_id", user.ID)
	return nil
}

// resolveTownName prefers the name the council set over the one the process was
// started with, and falls back to a neutral label rather than an empty string —
// a subject line reading "Ana invited you to join " is worse than one naming
// nothing in particular.
func (s *InviteService) resolveTownName(ctx context.Context) string {
	if name, err := s.config.GetTownConfig(ctx, "town_name"); err == nil {
		if name = strings.TrimSpace(name); name != "" {
			return name
		}
	}
	if s.townName != "" {
		return s.townName
	}
	return "The Bell"
}

// inviteURL builds the link the invitee follows.
//
// When PUBLIC_URL is unset the link is returned as a site-relative path. The
// application genuinely does not know its own public address in that
// configuration — the root compose gives Kratos a public URL and the app none —
// and a link built on a guess (the request's Host header, say) is a link an
// attacker can point wherever they like by sending one request. A relative path
// is honest, the frontend absolutizes it against the origin the member is
// already looking at, and the emailed copy is the only casualty: mail needs an
// absolute link, which is why the deployment docs make PUBLIC_URL part of
// turning invitation mail on.
func (s *InviteService) inviteURL(rawToken string) string {
	path := "/auth/registration?invite=" + url.QueryEscape(rawToken)
	if s.publicURL == "" {
		return path
	}
	return s.publicURL + path
}

// inviterName renders the inviter for a subject line, falling back to something
// sayable when they have never set a display name.
func inviterName(inviter *domain.User) string {
	if inviter != nil && strings.TrimSpace(inviter.DisplayName) != "" {
		return strings.TrimSpace(inviter.DisplayName)
	}
	return "A neighbour"
}

// inviteEmailBody writes the message an invitee actually reads.
//
// Plain text, and warm rather than transactional, because of who receives it:
// somebody who has no account, may never have heard of The Bell, and is being
// told a neighbour vouched for them. The note is quoted when there is one — it
// is the only part of the message a person wrote — and the vouch is spelled out
// at the end, since "accepting this also makes them responsible for you" is the
// unusual part of joining and should not be a surprise discovered later.
func inviteEmailBody(inviterName, townName, note, link string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s has invited you to join %s on The Bell.\n\n", inviterName, townName)
	b.WriteString("The Bell is a small notice board for one town: neighbours post what is " +
		"happening, and membership runs on people vouching for people they know rather " +
		"than on anyone signing up.\n\n")
	if note != "" {
		fmt.Fprintf(&b, "%s added a note for you:\n\n    %s\n\n", inviterName, note)
	}
	fmt.Fprintf(&b, "Accept the invitation here:\n\n    %s\n\n", link)
	b.WriteString("The link works for the next 14 days, and only for this email address.\n\n")
	fmt.Fprintf(&b, "Accepting also records %s vouching for you, which is what makes you a "+
		"member of %s straight away.\n\n", inviterName, townName)
	b.WriteString("If you were not expecting this, you can ignore it — nothing happens until " +
		"you follow the link.\n")
	return b.String()
}

// normalizeEmail is the one place an address is folded for comparison: trimmed
// and lowercased. Every query matches on lower(email), and invitations are
// stored in this form, so what is stored, what is matched at redemption and
// what the registration gate compares against the submitted traits are all the
// same string.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// validateInviteEmail applies basic sanity to an address, and deliberately
// stops well short of RFC 5322.
//
// A full parser would accept quoted local parts, comments and address groups
// that no invitee will ever type, and would still not tell anybody whether the
// address receives mail — the only test that matters, and the one the send
// itself performs. What this catches is the mistake a member actually makes: a
// typo, a display name pasted in with the address, a missing domain. Anything
// it rejects, the member is told about; anything it accepts and the relay then
// bounces comes back as email_sent:false with the link still in hand.
func validateInviteEmail(email string) error {
	if email == "" {
		return fmt.Errorf("%w: email is required", ErrValidation)
	}
	if len(email) > maxEmailLength {
		return fmt.Errorf("%w: email address is too long", ErrValidation)
	}
	if strings.ContainsAny(email, " \t\r\n\"'<>,;()[]\\") {
		return fmt.Errorf("%w: email address contains characters that are not allowed", ErrValidation)
	}

	local, domainPart, found := strings.Cut(email, "@")
	if !found || strings.Contains(domainPart, "@") {
		return fmt.Errorf("%w: email address must contain exactly one @", ErrValidation)
	}
	if local == "" {
		return fmt.Errorf("%w: email address has no local part before the @", ErrValidation)
	}
	if !strings.Contains(domainPart, ".") {
		return fmt.Errorf("%w: email address needs a domain like example.com", ErrValidation)
	}
	if strings.HasPrefix(domainPart, ".") || strings.HasSuffix(domainPart, ".") ||
		strings.Contains(domainPart, "..") || strings.HasPrefix(domainPart, "-") {
		return fmt.Errorf("%w: email address domain is not valid", ErrValidation)
	}
	return nil
}

// newInviteToken returns the raw token to hand out and the hash to store.
//
// base64url without padding so the value survives a URL, an email client's
// linkifier and a copy-paste unchanged. crypto/rand, never math/rand: this
// token is a credential — presenting it is what gets somebody through the
// registration gate — so a value derivable from the process start time would be
// a way into an invite-only town.
func newInviteToken() (raw, hash string, err error) {
	b := make([]byte, inviteTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generating invite token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, hashInviteToken(raw), nil
}

// hashInviteToken is the one definition of how a raw token maps to what is
// stored. Plain SHA-256, no salt and no stretching, and both omissions are
// deliberate: the token is 256 bits of uniform randomness, so there is no
// guessable input for a dictionary attack to work through, and a per-row salt
// would make lookup by token impossible without scanning the table.
func hashInviteToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

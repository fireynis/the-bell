package domain

import "time"

// InviteStatus is what an invitation looks like to the person who sent it.
//
// It is derived from timestamps at read time rather than stored, because three
// of the four states are facts about the clock rather than events anybody
// records. Storing it would mean something had to sweep the table to turn open
// invitations into expired ones, and an expiry that depends on a sweep is an
// expiry that stops happening the moment the sweep does.
type InviteStatus string

const (
	InviteOpen     InviteStatus = "open"
	InviteAccepted InviteStatus = "accepted"
	InviteExpired  InviteStatus = "expired"
	InviteRevoked  InviteStatus = "revoked"
)

// Invite is one member's invitation to an address, which is also their vouch
// for whoever answers it.
//
// The raw token is deliberately absent: only its SHA-256 hash is ever stored,
// and the raw value is returned exactly once by the call that created the
// invitation. Nothing that reads an invitation back out of the database can
// reconstruct a working link, which is why the listing endpoint can be an
// ordinary authenticated read.
//
// InviterDisplayName and ConsumedByDisplayName are joined in by the queries
// that serve people rather than machinery; the create path leaves both empty
// because it knows the inviter by id and nobody has accepted yet.
type Invite struct {
	ID        string
	Email     string
	Note      string
	InviterID string
	CreatedAt time.Time
	ExpiresAt time.Time

	ConsumedAt *time.Time
	ConsumedBy string
	RevokedAt  *time.Time

	InviterDisplayName    string
	ConsumedByDisplayName string
}

// Status reports how the invitation stands at now.
//
// The order of the tests is the order the events actually settle in. Acceptance
// wins outright: an invitation that was redeemed is history, and whether its
// expiry has since passed says nothing about it.
//
// Revocation is compared against the expiry rather than simply reported,
// because withdrawing an invitation and reaping a dead one write the same
// column. InviteService.Create reaps an expired invitation by stamping
// revoked_at — that is how an address is freed for a fresh invitation without
// the unique index ever seeing two live rows — and a reaped row is one whose
// revoked_at is at or after its expires_at. Such a row reads as expired, which
// is what happened to it; only a withdrawal made while the invitation was still
// open reads as revoked.
func (i *Invite) Status(now time.Time) InviteStatus {
	switch {
	case i.ConsumedAt != nil:
		return InviteAccepted
	case i.RevokedAt != nil && i.RevokedAt.Before(i.ExpiresAt):
		return InviteRevoked
	case !now.Before(i.ExpiresAt):
		return InviteExpired
	case i.RevokedAt != nil:
		// Unreachable through any path this codebase writes — a reap stamps a
		// time already past expires_at, which the branch above catches — and
		// kept so that a row written by hand, or by a clock that jumped
		// backwards, still reads as withdrawn rather than open.
		return InviteRevoked
	default:
		return InviteOpen
	}
}

// IsLive reports whether the invitation can still be redeemed at now. It is the
// Go statement of the liveness clause the queries apply in SQL; the two must
// agree, and the queries are the authority since they are what a concurrent
// redemption races against.
func (i *Invite) IsLive(now time.Time) bool {
	return i.Status(now) == InviteOpen
}

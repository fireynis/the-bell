package domain

import "time"

type VouchStatus string

const (
	VouchActive  VouchStatus = "active"
	VouchRevoked VouchStatus = "revoked"
)

// Vouch is one member's endorsement of another.
//
// The json tags matter: this struct is serialized directly by the create
// endpoint, while the profile listing serializes a separate vouchEntry DTO.
// Without tags the two describe the same entity in different casing — Go
// clients cannot tell, because they decode into this struct at both ends, but
// every other client sees ID here and id there.
type Vouch struct {
	ID        string      `json:"id"`
	VoucherID string      `json:"voucher_id"` // person giving the vouch
	VoucheeID string      `json:"vouchee_id"` // person receiving the vouch
	Status    VouchStatus `json:"status"`
	CreatedAt time.Time   `json:"created_at"`
	RevokedAt *time.Time  `json:"revoked_at,omitempty"`

	// VoucherDisplayName and VoucheeDisplayName name both parties, joined in by
	// the profile listing queries. Without them a vouch list is a column of
	// UUIDs: the endorsement is the whole point of the trust graph, and it means
	// nothing rendered as "0193a7b2...".
	//
	// They follow Post.AuthorDisplayName exactly — omitempty, and populated by
	// the list reads alone. The vouch a POST /v1/vouches response echoes back
	// carries neither, because the create path knows both people by id only.
	//
	// The profile listing does not serialize this struct: it builds vouchEntry,
	// which always populates both names and so sends both keys unconditionally,
	// empty string included.
	VoucherDisplayName string `json:"voucher_display_name,omitempty"`
	VoucheeDisplayName string `json:"vouchee_display_name,omitempty"`
}

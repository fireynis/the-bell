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
}

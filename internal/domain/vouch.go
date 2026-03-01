package domain

import "time"

type VouchStatus string

const (
	VouchActive  VouchStatus = "active"
	VouchRevoked VouchStatus = "revoked"
)

type Vouch struct {
	ID        string
	VoucherID string // person giving the vouch
	VoucheeID string // person receiving the vouch
	Status    VouchStatus
	CreatedAt time.Time
	RevokedAt *time.Time
}

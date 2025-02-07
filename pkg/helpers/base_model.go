package helpers

import (
	"database/sql"
	"time"

	"gorm.io/gorm"
)

type Base struct {
	ID        string       `json:"id" gorm:"primaryKey"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
	DeletedAt sql.NullTime `json:"deleted_at"`
}

func (b *Base) BeforeCreate(tx *gorm.DB) (err error) {
	b.ID = GenerateTSUUID()
	return
}

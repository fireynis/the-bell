package models

import (
	"database/sql"
	"github.com/fireynis/the-bell-api/pkg/helpers"
	"gorm.io/gorm"
	"time"
)

type Base struct {
	ID        string       `json:"id" gorm:"primaryKey"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
	DeletedAt sql.NullTime `json:"deleted_at"`
}

func (b *Base) BeforeCreate(tx *gorm.DB) (err error) {
	b.ID = helpers.GenerateUUID()
	return
}

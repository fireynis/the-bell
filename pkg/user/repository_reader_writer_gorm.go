package user

import (
	"context"

	"github.com/fireynis/the-bell-api/pkg/errors"
	"gorm.io/gorm"
)

// GormUserRepository implements the Repository interface using GORM
type GormUserRepository struct {
	db *gorm.DB
}

// NewGormUserRepository creates a new GORM user repository
func NewGormUserRepository(db *gorm.DB) Repository {
	return &GormUserRepository{db: db}
}

// GetByID retrieves a user by ID
func (r *GormUserRepository) GetByID(ctx context.Context, id string) (User, error) {
	var user User
	result := r.db.WithContext(ctx).Where("id = ?", id).First(&user)
	if result.Error != nil {
		return User{}, errors.ParseError(result.Error)
	}
	return user, nil
}

// GetByEmail retrieves a user by email
func (r *GormUserRepository) GetByEmail(ctx context.Context, email string) (User, error) {
	var user User
	result := r.db.WithContext(ctx).Where("email = ?", email).First(&user)
	if result.Error != nil {
		return User{}, errors.ParseError(result.Error)
	}
	return user, nil
}

// Create creates a new user
func (r *GormUserRepository) Create(ctx context.Context, user *User) error {
	result := r.db.WithContext(ctx).Create(user)
	return errors.ParseError(result.Error)
}

// Update updates an existing user
func (r *GormUserRepository) Update(ctx context.Context, user *User) error {
	result := r.db.WithContext(ctx).Save(user)
	return errors.ParseError(result.Error)
}

// Delete soft deletes a user by ID
func (r *GormUserRepository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Where("id = ?", id).Delete(&User{})
	if result.Error != nil {
		return errors.ParseError(result.Error)
	}
	if result.RowsAffected == 0 {
		return errors.ErrNotFound
	}
	return nil
}

// HardDelete permanently deletes a user by ID
func (r *GormUserRepository) HardDelete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Unscoped().Where("id = ?", id).Delete(&User{})
	if result.Error != nil {
		return errors.ParseError(result.Error)
	}
	if result.RowsAffected == 0 {
		return errors.ErrNotFound
	}
	return nil
}

package user

import (
	"context"

	"github.com/fireynis/the-bell-api/pkg/errors"
	"github.com/fireynis/the-bell-api/pkg/field_condition"
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

// List retrieves a list of users based on conditions
func (r *GormUserRepository) List(ctx context.Context, conditions ...field_condition.FieldCondition) ([]User, error) {
	var users []User
	query := r.db.WithContext(ctx)

	// TODO: Implement field conditions when needed
	// For now, just return all users with a limit
	result := query.Limit(100).Find(&users)
	if result.Error != nil {
		return nil, errors.ParseError(result.Error)
	}

	return users, nil
}

// Count returns the number of users matching the conditions
func (r *GormUserRepository) Count(ctx context.Context, conditions ...field_condition.FieldCondition) (int64, error) {
	var count int64
	query := r.db.WithContext(ctx).Model(&User{})

	// TODO: Implement field conditions when needed
	result := query.Count(&count)
	if result.Error != nil {
		return 0, errors.ParseError(result.Error)
	}

	return count, nil
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

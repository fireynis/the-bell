package user

import (
	"context"

	"github.com/fireynis/the-bell-api/pkg/field_condition"
)

// ServiceReader defines the interface for reading users from the service layer
type ServiceReader interface {
	// GetByID retrieves a user by ID
	GetByID(ctx context.Context, id string) (User, error)

	// GetByEmail retrieves a user by email
	GetByEmail(ctx context.Context, email string) (User, error)

	// List retrieves a list of users based on conditions
	List(ctx context.Context, conditions ...field_condition.FieldCondition) ([]User, error)

	// Count returns the number of users matching the conditions
	Count(ctx context.Context, conditions ...field_condition.FieldCondition) (int64, error)
}

// ServiceWriter defines the interface for writing users in the service layer
type ServiceWriter interface {
	// Create creates a new user with password hashing
	Create(ctx context.Context, input CreateUserInput) (User, error)

	// Update updates an existing user
	Update(ctx context.Context, id string, input UpdateUserInput) (User, error)

	// Delete soft deletes a user by ID
	Delete(ctx context.Context, id string) error

	// VerifyPassword verifies if a password matches the user's hashed password
	VerifyPassword(ctx context.Context, email string, password string) (User, error)

	// ChangePassword changes a user's password
	ChangePassword(ctx context.Context, id string, oldPassword string, newPassword string) error
}

// Service combines reader and writer interfaces
type Service interface {
	ServiceReader
	ServiceWriter
}

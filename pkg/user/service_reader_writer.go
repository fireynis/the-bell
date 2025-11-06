package user

import (
	"context"
)

// ServiceReader defines the interface for reading users from the service layer
type ServiceReader interface {
	// GetByID retrieves a user by ID
	GetByID(ctx context.Context, id string) (User, error)

	// GetByEmail retrieves a user by email
	GetByEmail(ctx context.Context, email string) (User, error)
}

// ServiceWriter defines the interface for writing users in the service layer
type ServiceWriter interface {
	// Create creates a new user with password hashing
	Create(ctx context.Context, input CreateUserInput) (User, error)

	// Update updates an existing user
	Update(ctx context.Context, id string, input UpdateUserInput) (User, error)

	// Delete soft deletes a user by ID
	Delete(ctx context.Context, id string) error

	// ChangePassword changes a user's password
	ChangePassword(ctx context.Context, id string, oldPassword string, newPassword string) error
}

// Service combines reader and writer interfaces
type Service interface {
	ServiceReader
	ServiceWriter
}

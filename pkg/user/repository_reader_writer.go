package user

import (
	"context"

	"github.com/fireynis/the-bell-api/pkg/field_condition"
)

// RepositoryReader defines the interface for reading users from the repository
type RepositoryReader interface {
	// GetByID retrieves a user by ID
	GetByID(ctx context.Context, id string) (User, error)

	// GetByEmail retrieves a user by email
	GetByEmail(ctx context.Context, email string) (User, error)

	// List retrieves a list of users based on conditions
	List(ctx context.Context, conditions ...field_condition.FieldCondition) ([]User, error)

	// Count returns the number of users matching the conditions
	Count(ctx context.Context, conditions ...field_condition.FieldCondition) (int64, error)
}

// RepositoryWriter defines the interface for writing users to the repository
type RepositoryWriter interface {
	// Create creates a new user
	Create(ctx context.Context, user *User) error

	// Update updates an existing user
	Update(ctx context.Context, user *User) error

	// Delete soft deletes a user by ID
	Delete(ctx context.Context, id string) error

	// HardDelete permanently deletes a user by ID
	HardDelete(ctx context.Context, id string) error
}

// Repository combines reader and writer interfaces
type Repository interface {
	RepositoryReader
	RepositoryWriter
}

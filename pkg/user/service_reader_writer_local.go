package user

import (
	"context"
	"strings"

	"github.com/fireynis/the-bell-api/pkg/errors"
	"golang.org/x/crypto/bcrypt"
)

// LocalUserService implements the Service interface
type LocalUserService struct {
	repo Repository
}

// NewLocalUserService creates a new local user service
func NewLocalUserService(repo Repository) Service {
	return &LocalUserService{
		repo: repo,
	}
}

// GetByID retrieves a user by ID
func (s *LocalUserService) GetByID(ctx context.Context, id string) (User, error) {
	if strings.TrimSpace(id) == "" {
		return User{}, errors.ErrInvalidQuery
	}

	return s.repo.GetByID(ctx, id)
}

// GetByEmail retrieves a user by email
func (s *LocalUserService) GetByEmail(ctx context.Context, email string) (User, error) {
	if strings.TrimSpace(email) == "" {
		return User{}, errors.ErrInvalidQuery
	}

	// Normalize email
	email = strings.ToLower(strings.TrimSpace(email))

	return s.repo.GetByEmail(ctx, email)
}

// Create creates a new user with password hashing
func (s *LocalUserService) Create(ctx context.Context, input CreateUserInput) (User, error) {
	// Validate input
	if err := input.Validate(); err != nil {
		return User{}, err
	}

	// Check if user with email already exists
	existingUser, err := s.repo.GetByEmail(ctx, strings.ToLower(strings.TrimSpace(input.Email)))
	if err == nil && existingUser.ID != "" {
		return User{}, errors.ValidationError{
			Errors: map[string]string{
				"email": "email already exists",
			},
		}
	} else if err != nil && err != errors.ErrNotFound {
		return User{}, err
	}

	// Hash the password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, errors.ErrUnexpected
	}

	// Create user
	user := User{
		Name:          input.Name,
		Email:         strings.ToLower(strings.TrimSpace(input.Email)),
		PasswordHash:  string(hashedPassword),
		EmailVerified: false,
		IsActive:      true,
	}

	// Validate user
	if err := user.Validate(); err != nil {
		return User{}, err
	}

	// Save to repository
	if err := s.repo.Create(ctx, &user); err != nil {
		return User{}, err
	}

	return user, nil
}

// Update updates an existing user
func (s *LocalUserService) Update(ctx context.Context, id string, input UpdateUserInput) (User, error) {
	if strings.TrimSpace(id) == "" {
		return User{}, errors.ErrInvalidQuery
	}

	// Validate input
	if err := input.Validate(); err != nil {
		return User{}, err
	}

	// Get existing user
	user, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return User{}, err
	}

	// Update fields if provided
	if input.Name != nil {
		user.Name = *input.Name
	}

	if input.EmailVerified != nil {
		user.EmailVerified = *input.EmailVerified
	}

	if input.IsActive != nil {
		user.IsActive = *input.IsActive
	}

	// Validate updated user
	if err := user.Validate(); err != nil {
		return User{}, err
	}

	// Save to repository
	if err := s.repo.Update(ctx, &user); err != nil {
		return User{}, err
	}

	return user, nil
}

// Delete soft deletes a user by ID
func (s *LocalUserService) Delete(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.ErrInvalidQuery
	}

	return s.repo.Delete(ctx, id)
}

// VerifyPassword verifies if a password matches the user's hashed password
func (s *LocalUserService) VerifyPassword(ctx context.Context, email string, password string) (User, error) {
	if strings.TrimSpace(email) == "" || strings.TrimSpace(password) == "" {
		return User{}, errors.ErrInvalidQuery
	}

	// Get user by email
	user, err := s.repo.GetByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return User{}, err
	}

	// Check if user is active
	if !user.IsActive {
		return User{}, errors.ValidationError{
			Errors: map[string]string{
				"user": "user account is inactive",
			},
		}
	}

	// Verify password
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		return User{}, errors.ValidationError{
			Errors: map[string]string{
				"password": "invalid email or password",
			},
		}
	}

	return user, nil
}

// ChangePassword changes a user's password
func (s *LocalUserService) ChangePassword(ctx context.Context, id string, oldPassword string, newPassword string) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(oldPassword) == "" || strings.TrimSpace(newPassword) == "" {
		return errors.ErrInvalidQuery
	}

	// Validate new password length
	if len(newPassword) < 8 {
		return errors.ValidationError{
			Errors: map[string]string{
				"password": "password must be at least 8 characters",
			},
		}
	}

	if len(newPassword) > 72 {
		return errors.ValidationError{
			Errors: map[string]string{
				"password": "password must be less than 72 characters",
			},
		}
	}

	// Get existing user
	user, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	// Verify old password
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword))
	if err != nil {
		return errors.ValidationError{
			Errors: map[string]string{
				"password": "invalid old password",
			},
		}
	}

	// Hash the new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return errors.ErrUnexpected
	}

	user.PasswordHash = string(hashedPassword)

	// Save to repository
	return s.repo.Update(ctx, &user)
}

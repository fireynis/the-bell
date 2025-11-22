package user

import (
	"regexp"
	"strings"

	"github.com/fireynis/the-bell-api/pkg/errors"
	"github.com/fireynis/the-bell-api/pkg/helpers"
)

// User represents a user in the system
type User struct {
	helpers.Base
	Name          string `json:"name" gorm:"type:varchar(255)"`
	Email         string `json:"email" gorm:"type:varchar(255);uniqueIndex;not null"`
	PasswordHash  string `json:"-" gorm:"type:varchar(255);not null"` // Never expose in JSON
	EmailVerified bool   `json:"email_verified" gorm:"default:false"`
	IsActive      bool   `json:"is_active" gorm:"default:true"`
}

// TableName specifies the table name for GORM
func (User) TableName() string {
	return "users"
}

// Validate validates the user fields
func (u *User) Validate() error {
	validationError := errors.ValidationError{
		Errors: make(map[string]string),
	}

	// Validate name
	if strings.TrimSpace(u.Name) == "" {
		validationError.Errors["name"] = "name is required"
	} else if len(u.Name) > 255 {
		validationError.Errors["name"] = "name must be less than 255 characters"
	}

	// Validate email
	if strings.TrimSpace(u.Email) == "" {
		validationError.Errors["email"] = "email is required"
	} else {
		// Email format validation
		emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
		if !emailRegex.MatchString(u.Email) {
			validationError.Errors["email"] = "invalid email format"
		} else if len(u.Email) > 255 {
			validationError.Errors["email"] = "email must be less than 255 characters"
		}
	}

	// Normalize email to lowercase
	u.Email = strings.ToLower(strings.TrimSpace(u.Email))

	// Validate password hash (should be set before validation)
	if strings.TrimSpace(u.PasswordHash) == "" {
		validationError.Errors["password"] = "password is required"
	}

	if len(validationError.Errors) > 0 {
		return validationError
	}

	return nil
}

// CreateUserInput represents the input for creating a user
type CreateUserInput struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Validate validates the create user input
func (c *CreateUserInput) Validate() error {
	validationError := errors.ValidationError{
		Errors: make(map[string]string),
	}

	// Validate name
	if strings.TrimSpace(c.Name) == "" {
		validationError.Errors["name"] = "name is required"
	} else if len(c.Name) > 255 {
		validationError.Errors["name"] = "name must be less than 255 characters"
	}

	// Validate email
	if strings.TrimSpace(c.Email) == "" {
		validationError.Errors["email"] = "email is required"
	} else {
		emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
		if !emailRegex.MatchString(c.Email) {
			validationError.Errors["email"] = "invalid email format"
		} else if len(c.Email) > 255 {
			validationError.Errors["email"] = "email must be less than 255 characters"
		}
	}

	// Validate password
	if strings.TrimSpace(c.Password) == "" {
		validationError.Errors["password"] = "password is required"
	} else if len(c.Password) < 8 {
		validationError.Errors["password"] = "password must be at least 8 characters"
	} else if len(c.Password) > 72 {
		// bcrypt has a 72 byte limit
		validationError.Errors["password"] = "password must be less than 72 characters"
	}

	if len(validationError.Errors) > 0 {
		return validationError
	}

	return nil
}

// UpdateUserInput represents the input for updating a user
type UpdateUserInput struct {
	Name          *string `json:"name,omitempty"`
	EmailVerified *bool   `json:"email_verified,omitempty"`
	IsActive      *bool   `json:"is_active,omitempty"`
}

// Validate validates the update user input
func (u *UpdateUserInput) Validate() error {
	validationError := errors.ValidationError{
		Errors: make(map[string]string),
	}

	// Validate name if provided
	if u.Name != nil {
		if strings.TrimSpace(*u.Name) == "" {
			validationError.Errors["name"] = "name cannot be empty"
		} else if len(*u.Name) > 255 {
			validationError.Errors["name"] = "name must be less than 255 characters"
		}
	}

	if len(validationError.Errors) > 0 {
		return validationError
	}

	return nil
}

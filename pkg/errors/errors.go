package errors

import (
	"errors"
	"fmt"
	"gorm.io/gorm"
)

var (
	ErrNotFound     = fmt.Errorf("resource not found")
	ErrUnexpected   = fmt.Errorf("unexpected error")
	ErrInvalidQuery = fmt.Errorf("invalid query")
	ErrNoUser       = fmt.Errorf("no user in context")
)

func ParseError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return ErrUnexpected
}

type ValidationError struct {
	ValidationErrors map[string]string
}

func (v ValidationError) Error() string {
	return fmt.Sprintf("validation error")
}

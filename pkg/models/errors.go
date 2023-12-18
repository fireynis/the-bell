package models

import "fmt"

var (
	ErrNotFound   = fmt.Errorf("resource not found")
	ErrUnexpected = fmt.Errorf("unexpected error")
)

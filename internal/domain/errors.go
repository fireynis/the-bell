package domain

import "errors"

// The error sentinels the whole application classifies failures by.
//
// These live here rather than in internal/service because the repository
// adapters raise them — a query that finds no row returns ErrNotFound, a unique
// violation returns ErrValidation. Declaring them in the service package meant
// persistence imported business logic to name its own failures, which inverts
// the dependency direction and pulled the entire service package into every
// repository test binary. Domain is the layer both sides already depend on.
//
// internal/service re-exports each of these under the same name, so
// errors.Is(err, service.ErrNotFound) keeps working; those aliases are the same
// values, not copies.
var (
	ErrNotFound   = errors.New("not found")
	ErrForbidden  = errors.New("forbidden")
	ErrValidation = errors.New("validation error")
	ErrEditWindow = errors.New("edit window expired")
	ErrRateLimit  = errors.New("rate limit exceeded")
)

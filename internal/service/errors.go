package service

import "github.com/fireynis/the-bell/internal/domain"

// The error sentinels are declared in internal/domain, so that the repository
// adapters can raise them without importing this package. These names are
// re-exported for the many existing errors.Is(err, service.ErrNotFound) call
// sites, statusForError in the handler package chief among them.
//
// Each of these is the SAME value as its domain counterpart, not a copy: an
// assignment shares the underlying error, which is what errors.Is compares.
// Re-declaring them with errors.New would compile and read identically while
// silently breaking every comparison in the codebase, so
// TestErrorSentinels_AreTheSameValuesAsDomain asserts the identity directly.
var (
	ErrNotFound   = domain.ErrNotFound
	ErrForbidden  = domain.ErrForbidden
	ErrValidation = domain.ErrValidation
	ErrEditWindow = domain.ErrEditWindow
	ErrRateLimit  = domain.ErrRateLimit
)

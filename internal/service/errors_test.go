package service

import (
	"errors"
	"fmt"
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
)

// The sentinels live in domain so that the repository adapters can return them
// without importing this package. The names here are aliases kept for the many
// existing errors.Is(err, service.ErrNotFound) call sites — statusForError in
// the handler package chief among them, which is what turns a sentinel into an
// HTTP status.
//
// An alias must be the SAME error value, not an equivalent one. Re-declaring
// these with errors.New would compile, read identically, and silently break
// every comparison in the codebase: errors.Is matches on equality, and two
// errors.New calls with the same text are never equal. That is the one way this
// move can go wrong, so it is asserted directly.
func TestErrorSentinels_AreTheSameValuesAsDomain(t *testing.T) {
	tests := []struct {
		name    string
		service error
		domain  error
	}{
		{"ErrNotFound", ErrNotFound, domain.ErrNotFound},
		{"ErrForbidden", ErrForbidden, domain.ErrForbidden},
		{"ErrValidation", ErrValidation, domain.ErrValidation},
		{"ErrEditWindow", ErrEditWindow, domain.ErrEditWindow},
		{"ErrRateLimit", ErrRateLimit, domain.ErrRateLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.service != tt.domain {
				t.Fatalf("service.%s is a different value from domain.%s (%v vs %v); "+
					"errors.Is comparisons across the two packages will all fail",
					tt.name, tt.name, tt.service, tt.domain)
			}
		})
	}
}

// The direction that matters in production: a repository adapter wraps and
// returns the domain sentinel, and a service or handler recognizes it through
// the service name.
func TestErrorSentinels_DomainErrorsMatchServiceNames(t *testing.T) {
	tests := []struct {
		name    string
		service error
		domain  error
	}{
		{"ErrNotFound", ErrNotFound, domain.ErrNotFound},
		{"ErrForbidden", ErrForbidden, domain.ErrForbidden},
		{"ErrValidation", ErrValidation, domain.ErrValidation},
		{"ErrEditWindow", ErrEditWindow, domain.ErrEditWindow},
		{"ErrRateLimit", ErrRateLimit, domain.ErrRateLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// What a repository returns: the domain sentinel, wrapped with
			// context the way the adapters do.
			fromRepo := fmt.Errorf("querying row: %w", tt.domain)
			if !errors.Is(fromRepo, tt.service) {
				t.Errorf("errors.Is(repo error, service.%s) = false; a 404 would surface as a 500", tt.name)
			}

			// And the reverse, for the service code that still raises these by
			// the service name.
			fromService := fmt.Errorf("checking: %w", tt.service)
			if !errors.Is(fromService, tt.domain) {
				t.Errorf("errors.Is(service error, domain.%s) = false", tt.name)
			}
		})
	}
}

// The sentinels must stay distinct from one another. Aliasing them all to a
// single value would pass the equality test above while collapsing every 404,
// 403 and 400 into the same status.
func TestErrorSentinels_AreDistinctFromEachOther(t *testing.T) {
	all := map[string]error{
		"ErrNotFound":   ErrNotFound,
		"ErrForbidden":  ErrForbidden,
		"ErrValidation": ErrValidation,
		"ErrEditWindow": ErrEditWindow,
		"ErrRateLimit":  ErrRateLimit,
	}

	for nameA, a := range all {
		for nameB, b := range all {
			if nameA == nameB {
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("errors.Is(%s, %s) = true; the two sentinels are not distinct", nameA, nameB)
			}
		}
	}
}

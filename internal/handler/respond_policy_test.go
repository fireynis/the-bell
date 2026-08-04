package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/fireynis/the-bell/internal/service"
)

func TestStatusForError(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantStatus  int
		wantMessage string
	}{
		{"not found", service.ErrNotFound, http.StatusNotFound, "not found"},
		{"forbidden", service.ErrForbidden, http.StatusForbidden, "forbidden"},
		{"rate limited", service.ErrRateLimit, http.StatusTooManyRequests, "rate limit exceeded"},
		{"edit window", service.ErrEditWindow, http.StatusConflict, "edit window expired"},
		{"missing reaction is a 404, not a server failure", service.ErrReactionNotFound, http.StatusNotFound, "reaction not found"},
		{"invalid reaction type is the caller's mistake", service.ErrInvalidReactionType, http.StatusBadRequest, "invalid reaction type"},
		{"unknown error", errors.New("connection refused"), http.StatusInternalServerError, "internal error"},
		{"nil error is treated as internal", nil, http.StatusInternalServerError, "internal error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, message := statusForError(tt.err)
			if status != tt.wantStatus {
				t.Errorf("status = %d, want %d", status, tt.wantStatus)
			}
			if message != tt.wantMessage {
				t.Errorf("message = %q, want %q", message, tt.wantMessage)
			}
		})
	}
}

// Wrapped errors must still map to their sentinel's status; the services wrap
// nearly every error they return with context.
func TestStatusForError_UnwrapsWrappedSentinels(t *testing.T) {
	tests := []struct {
		sentinel   error
		wantStatus int
	}{
		{service.ErrNotFound, http.StatusNotFound},
		{service.ErrForbidden, http.StatusForbidden},
		{service.ErrRateLimit, http.StatusTooManyRequests},
		{service.ErrEditWindow, http.StatusConflict},
		{service.ErrReactionNotFound, http.StatusNotFound},
		{service.ErrInvalidReactionType, http.StatusBadRequest},
	}

	for _, tt := range tests {
		wrapped := fmt.Errorf("looking up user: %w", fmt.Errorf("layer two: %w", tt.sentinel))
		if status, _ := statusForError(wrapped); status != tt.wantStatus {
			t.Errorf("doubly-wrapped %v: status = %d, want %d", tt.sentinel, status, tt.wantStatus)
		}
	}
}

// Validation errors are the only ones whose text reaches the client, because
// they describe what the caller got wrong.
func TestStatusForError_ValidationExposesItsMessage(t *testing.T) {
	err := fmt.Errorf("%w: body exceeds 1000 characters", service.ErrValidation)

	status, message := statusForError(err)
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", status, http.StatusBadRequest)
	}
	if !strings.Contains(message, "body exceeds 1000 characters") {
		t.Errorf("message = %q, want it to carry the validation detail", message)
	}
}

// An unrecognized error must never surface its text, which could leak database
// or infrastructure detail through the API.
func TestStatusForError_InternalErrorsDoNotLeakDetail(t *testing.T) {
	leaky := errors.New(`pq: relation "users" does not exist on host db-primary-1`)

	status, message := statusForError(leaky)
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", status, http.StatusInternalServerError)
	}
	if message != "internal error" {
		t.Errorf("message = %q, want the fixed %q", message, "internal error")
	}
	if strings.Contains(message, "users") || strings.Contains(message, "db-primary-1") {
		t.Errorf("message %q leaked internal detail", message)
	}
}

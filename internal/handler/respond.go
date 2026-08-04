package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/fireynis/the-bell/internal/httpjson"
	"github.com/fireynis/the-bell/internal/service"
)

// JSON marshals data and writes it as a JSON response with the given status code.
// If marshaling fails, it writes a 500 error instead.
func JSON(w http.ResponseWriter, status int, data any) {
	httpjson.Write(w, status, data)
}

// Error writes a JSON error response with the given status code and message.
func Error(w http.ResponseWriter, status int, message string) {
	httpjson.WriteError(w, status, message)
}

// Decode reads the request body into dst, rejecting unknown fields.
func Decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// statusForError maps a service-layer error onto the HTTP status and the
// message sent to the client.
//
// Only the errors describing what the caller got wrong expose their text.
// Everything else is reported with a fixed message so that internal failures
// cannot leak database or infrastructure detail through the API.
func statusForError(err error) (status int, message string) {
	switch {
	case errors.Is(err, service.ErrNotFound):
		return http.StatusNotFound, "not found"
	case errors.Is(err, service.ErrReactionNotFound):
		return http.StatusNotFound, "reaction not found"
	case errors.Is(err, service.ErrForbidden):
		return http.StatusForbidden, "forbidden"
	case errors.Is(err, service.ErrRateLimit):
		return http.StatusTooManyRequests, "rate limit exceeded"
	case errors.Is(err, service.ErrValidation):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, service.ErrInvalidReactionType):
		// Carries the rejected type, which is the caller's own input.
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, service.ErrEditWindow):
		return http.StatusConflict, "edit window expired"
	default:
		return http.StatusInternalServerError, "internal error"
	}
}

func serviceError(w http.ResponseWriter, err error) {
	status, message := statusForError(err)
	Error(w, status, message)
}

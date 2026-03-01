package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/fireynis/the-bell/internal/service"
)

// JSON marshals data and writes it as a JSON response with the given status code.
// If marshaling fails, it writes a 500 error instead.
func JSON(w http.ResponseWriter, status int, data any) {
	buf, err := json.Marshal(data)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal error"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(buf)
}

// Error writes a JSON error response with the given status code and message.
func Error(w http.ResponseWriter, status int, message string) {
	JSON(w, status, map[string]string{"error": message})
}

// Decode reads the request body into dst, rejecting unknown fields.
func Decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func serviceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrNotFound):
		Error(w, http.StatusNotFound, "not found")
	case errors.Is(err, service.ErrForbidden):
		Error(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, service.ErrRateLimit):
		Error(w, http.StatusTooManyRequests, "rate limit exceeded")
	case errors.Is(err, service.ErrValidation):
		Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrEditWindow):
		Error(w, http.StatusConflict, "edit window expired")
	default:
		Error(w, http.StatusInternalServerError, "internal error")
	}
}

// Package httpjson writes JSON HTTP responses.
//
// It exists so that every layer of the server emits byte-identical output for
// the same value. The handler and middleware packages both need to write the
// {"error": "..."} shape, but middleware cannot import handler — handler
// already imports middleware — so the shared implementation lives here, below
// them both.
package httpjson

import (
	"encoding/json"
	"net/http"
)

// internalErrorBody is written when marshaling the caller's value fails. It is
// a literal because at that point we already know the marshaler is unhappy.
const internalErrorBody = `{"error":"internal error"}`

// Write marshals data and writes it with the given status code. If marshaling
// fails it writes a 500 with a fixed error body instead.
//
// The body is written with json.Marshal rather than json.Encoder, so there is
// no trailing newline.
func Write(w http.ResponseWriter, status int, data any) {
	buf, err := json.Marshal(data)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(internalErrorBody))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(buf)
}

// WriteError writes a JSON error response with the given status and message.
func WriteError(w http.ResponseWriter, status int, message string) {
	Write(w, status, map[string]string{"error": message})
}

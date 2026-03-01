package handler

import "net/http"

// Health responds with a JSON status indicating the service is running.
func Health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

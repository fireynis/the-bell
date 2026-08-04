package handler

import "net/http"

// Health responds with a JSON status indicating the service is running.
func Health(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

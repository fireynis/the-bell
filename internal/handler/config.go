package handler

import (
	"net/http"

	"github.com/fireynis/the-bell/internal/service"
)

type ConfigHandler struct {
	config service.ConfigRepository
}

func NewConfigHandler(config service.ConfigRepository) *ConfigHandler {
	return &ConfigHandler{config: config}
}

// allowedConfigKeys is the set of town_config keys an admin may write through
// the API. Everything else (bootstrap_mode in particular) is owned by the
// server and must not be settable by a request.
var allowedConfigKeys = map[string]bool{
	"town_name":     true,
	"primary_color": true,
	"accent_color":  true,
}

// publicTownConfig returns the config entries safe to hand to any caller.
// bootstrap_mode is withheld because it tells an unauthenticated visitor that
// the town has not been claimed yet. The input map is left untouched.
func publicTownConfig(cfg map[string]string) map[string]string {
	public := make(map[string]string, len(cfg))
	for k, v := range cfg {
		if k == "bootstrap_mode" {
			continue
		}
		public[k] = v
	}
	return public
}

// validateConfigUpdate returns the first key in req that may not be written,
// or "" when the whole request is acceptable. Callers must check the entire
// request before writing any of it: map iteration order is random, so writing
// as we validate would apply a random prefix of a rejected request.
func validateConfigUpdate(req map[string]string) (badKey string) {
	for k := range req {
		if !allowedConfigKeys[k] {
			return k
		}
	}
	return ""
}

func (h *ConfigHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.config.ListTownConfig(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to load config")
		return
	}
	JSON(w, http.StatusOK, publicTownConfig(cfg))
}

func (h *ConfigHandler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if badKey := validateConfigUpdate(req); badKey != "" {
		Error(w, http.StatusBadRequest, "key not allowed: "+badKey)
		return
	}
	for k, v := range req {
		if err := h.config.SetTownConfig(r.Context(), k, v); err != nil {
			Error(w, http.StatusInternalServerError, "failed to save config")
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

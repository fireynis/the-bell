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

func (h *ConfigHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.config.ListTownConfig(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to load config")
		return
	}
	public := make(map[string]string, len(cfg))
	for k, v := range cfg {
		if k == "bootstrap_mode" {
			continue
		}
		public[k] = v
	}
	JSON(w, http.StatusOK, public)
}

func (h *ConfigHandler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	allowed := map[string]bool{
		"town_name":     true,
		"primary_color": true,
		"accent_color":  true,
	}
	for k, v := range req {
		if !allowed[k] {
			Error(w, http.StatusBadRequest, "key not allowed: "+k)
			return
		}
		if err := h.config.SetTownConfig(r.Context(), k, v); err != nil {
			Error(w, http.StatusInternalServerError, "failed to save config")
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

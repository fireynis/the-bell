package handler

import (
	"log/slog"
	"net/http"

	"github.com/fireynis/the-bell/internal/service"
)

type ConfigHandler struct {
	config service.ConfigRepository
	tx     service.Transactor
}

// NewConfigHandler creates a ConfigHandler.
//
// The transactor is what makes a multi-key update atomic; config is still
// needed for the read path, which wants no transaction. A nil transactor
// disables writes rather than falling back to the unprotected loop — see
// UpdateConfig.
func NewConfigHandler(config service.ConfigRepository, tx service.Transactor) *ConfigHandler {
	return &ConfigHandler{config: config, tx: tx}
}

// allowedConfigKeys is the set of town_config keys an admin may write through
// the API. Everything else (bootstrap_mode in particular) is owned by the
// server and must not be settable by a request.
var allowedConfigKeys = map[string]bool{
	"town_name":         true,
	"primary_color":     true,
	"accent_color":      true,
	"registration_mode": true,
}

// allowedConfigValues constrains the keys whose value is a choice rather than
// free text.
//
// registration_mode is the first such key, and it needs the constraint more
// than a colour does: it decides whether strangers can create accounts, and a
// typo would not fail — the registration gate treats anything that is not
// "open" as invite-only, so "opne" would silently close the town, and a gate
// written the other way round would silently open it. Rejecting the value at
// the edge means the council sees their mistake immediately.
//
// A key absent from this map accepts any value, which is what town_name and the
// colours want.
var allowedConfigValues = map[string]map[string]bool{
	"registration_mode": {
		service.RegistrationModeInvite: true,
		service.RegistrationModeOpen:   true,
	},
}

// publicTownConfig returns the config entries safe to hand to any caller.
// bootstrap_mode is withheld because it tells an unauthenticated visitor that
// the town has not been claimed yet. The input map is left untouched.
//
// registration_mode is deliberately NOT withheld. The sign-in and sign-up
// screens are the least authenticated pages there are and they have to know
// which one to offer: without it, an invite-only town shows a "create an
// account" form that always ends in 403. It also gives away nothing — anybody
// can learn the same fact by trying to register once.
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

// validateConfigUpdate returns a message describing the first entry in req that
// may not be written, or "" when the whole request is acceptable. Callers must
// check the entire request before writing any of it: map iteration order is
// random, so writing as we validate would apply a random prefix of a rejected
// request.
func validateConfigUpdate(req map[string]string) (problem string) {
	for k, v := range req {
		if !allowedConfigKeys[k] {
			return "key not allowed: " + k
		}
		if allowed, constrained := allowedConfigValues[k]; constrained && !allowed[v] {
			return "value not allowed for " + k + ": " + v
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

// UpdateConfig writes the supplied keys, all of them or none.
//
// The whole request is validated before the first write, and the writes then
// run inside one transaction. Both halves are needed: without the validation
// pass a rejected request would still apply a prefix of itself, and without the
// transaction a write failing partway would leave the earlier keys applied
// while the response said 500. Map iteration order is random, so in both cases
// *which* keys survived differed between runs — an admin could not tell from
// the response what state the town config was actually in.
func (h *ConfigHandler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if problem := validateConfigUpdate(req); problem != "" {
		Error(w, http.StatusBadRequest, problem)
		return
	}
	if h.tx == nil {
		// Refusing beats writing without a transaction. A server wired without
		// one would otherwise silently reintroduce the partial-write bug, and
		// the only signal would be a half-applied config after an outage.
		slog.Error("config update refused: handler has no transactor")
		Error(w, http.StatusInternalServerError, "failed to save config")
		return
	}

	err := h.tx.InTx(r.Context(), func(repos service.RepoSet) error {
		config := repos.Config()
		for k, v := range req {
			if err := config.SetTownConfig(r.Context(), k, v); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		slog.Error("saving town config", "error", err, "keys", len(req))
		Error(w, http.StatusInternalServerError, "failed to save config")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

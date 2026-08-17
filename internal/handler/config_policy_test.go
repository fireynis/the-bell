package handler

import (
	"reflect"
	"testing"
)

func TestPublicTownConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  map[string]string
		want map[string]string
	}{
		{"empty config yields empty map", map[string]string{}, map[string]string{}},
		{"nil config yields empty map", nil, map[string]string{}},
		{
			"bootstrap_mode is withheld",
			map[string]string{"town_name": "Bellville", "bootstrap_mode": "true"},
			map[string]string{"town_name": "Bellville"},
		},
		{
			"everything else passes through",
			map[string]string{"town_name": "Bellville", "primary_color": "#123456", "accent_color": "#abcdef"},
			map[string]string{"town_name": "Bellville", "primary_color": "#123456", "accent_color": "#abcdef"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := publicTownConfig(tt.cfg); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("publicTownConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}

// The caller's map is the repository's own cached view of town_config, so
// filtering must not delete anything from it.
func TestPublicTownConfig_DoesNotMutateInput(t *testing.T) {
	cfg := map[string]string{"town_name": "Bellville", "bootstrap_mode": "true"}

	publicTownConfig(cfg)

	if len(cfg) != 2 {
		t.Fatalf("input map has %d keys after filtering, want 2: %v", len(cfg), cfg)
	}
	if cfg["bootstrap_mode"] != "true" {
		t.Errorf("bootstrap_mode = %q in the input map, want it left intact", cfg["bootstrap_mode"])
	}
}

func TestValidateConfigUpdate(t *testing.T) {
	tests := []struct {
		name string
		req  map[string]string
		want string
	}{
		{"empty request is acceptable", map[string]string{}, ""},
		{"single allowed key", map[string]string{"town_name": "Bellville"}, ""},
		{
			"every allowed key at once",
			map[string]string{
				"town_name": "Bellville", "primary_color": "#123456",
				"accent_color": "#abcdef", "registration_mode": "invite",
			},
			"",
		},
		{"unknown key is rejected", map[string]string{"favourite_bell": "big"}, "key not allowed: favourite_bell"},
		{"bootstrap_mode is not writable", map[string]string{"bootstrap_mode": "false"}, "key not allowed: bootstrap_mode"},
		{
			"one bad key rejects the whole request",
			map[string]string{"town_name": "Bellville", "bootstrap_mode": "false"},
			"key not allowed: bootstrap_mode",
		},
		{"registration_mode may be open", map[string]string{"registration_mode": "open"}, ""},
		{
			"registration_mode rejects anything else",
			map[string]string{"registration_mode": "opne"},
			"value not allowed for registration_mode: opne",
		},
		{
			"registration_mode is case sensitive, because the gate's comparison is exact",
			map[string]string{"registration_mode": "Open"},
			"value not allowed for registration_mode: Open",
		},
		{
			"an unconstrained key still takes any value",
			map[string]string{"town_name": "anything at all"},
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validateConfigUpdate(tt.req); got != tt.want {
				t.Errorf("validateConfigUpdate() = %q, want %q", got, tt.want)
			}
		})
	}
}

// registration_mode has to reach the sign-up screen, which has no session and
// so no other way to learn whether it should offer a registration form at all.
func TestPublicTownConfig_IncludesRegistrationMode(t *testing.T) {
	public := publicTownConfig(map[string]string{
		"registration_mode": "invite",
		"bootstrap_mode":    "true",
	})

	if public["registration_mode"] != "invite" {
		t.Errorf("registration_mode = %q, want it published", public["registration_mode"])
	}
	if _, found := public["bootstrap_mode"]; found {
		t.Error("bootstrap_mode is in the public config, want it withheld")
	}
}

package config_test

import (
	"os"
	"strings"
	"testing"

	"github.com/fireynis/the-bell/internal/config"
)

// unset removes an env var for the duration of the test. t.Setenv cannot express
// "absent", and absence is exactly what a default-value test has to assert.
func unset(t *testing.T, key string) {
	t.Helper()
	if old, ok := os.LookupEnv(key); ok {
		t.Cleanup(func() { os.Setenv(key, old) })
	}
	os.Unsetenv(key)
}

func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://localhost:5432/bell")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("KRATOS_PUBLIC_URL", "http://localhost:4433")
	t.Setenv("KRATOS_ADMIN_URL", "http://localhost:4434")
}

func TestLoad_Defaults(t *testing.T) {
	setRequired(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.ImageStoragePath != "/storage/the-bell/images" {
		t.Errorf("ImageStoragePath = %q, want %q", cfg.ImageStoragePath, "/storage/the-bell/images")
	}
	if cfg.TownName != "My Town" {
		t.Errorf("TownName = %q, want %q", cfg.TownName, "My Town")
	}
}

// The dev relaxations gated behind IsDev include dropping the Secure attribute
// from Kratos session cookies. An unset BELL_ENV — the state of every
// deployment that predates the variable — must therefore mean production.
func TestLoad_DefaultsToProduction(t *testing.T) {
	setRequired(t)
	unset(t, "BELL_ENV")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Env != "production" {
		t.Errorf("Env = %q, want %q", cfg.Env, "production")
	}
	if cfg.IsDev() {
		t.Error("IsDev() = true with BELL_ENV unset, want false")
	}
}

func TestConfig_IsDev(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want bool
	}{
		{"dev", "dev", true},
		{"development", "development", true},
		{"case is ignored", "DEV", true},
		{"surrounding whitespace is ignored", "  development  ", true},
		{"production", "production", false},
		{"staging is not dev", "staging", false},
		{"empty is not dev", "", false},
		// Anything unrecognised is production: a typo must not be what
		// downgrades a live deployment's cookie security.
		{"typo is not dev", "devel", false},
		{"local is not dev", "local", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{Env: tt.env}
			if got := cfg.IsDev(); got != tt.want {
				t.Errorf("IsDev() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoad_EnvFromEnvironment(t *testing.T) {
	setRequired(t)
	t.Setenv("BELL_ENV", "development")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.IsDev() {
		t.Errorf("IsDev() = false with BELL_ENV=development, want true")
	}
}

func TestLoad_MissingRequired(t *testing.T) {
	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for missing required env vars, got nil")
	}
}

func TestLoad_AllCustomValues(t *testing.T) {
	t.Setenv("PORT", "3000")
	t.Setenv("DATABASE_URL", "postgres://custom:5432/bell")
	t.Setenv("REDIS_URL", "redis://custom:6379")
	t.Setenv("KRATOS_PUBLIC_URL", "http://custom:4433")
	t.Setenv("KRATOS_ADMIN_URL", "http://custom:4434")
	t.Setenv("IMAGE_STORAGE_PATH", "/tmp/images")
	t.Setenv("TOWN_NAME", "Springfield")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Port != 3000 {
		t.Errorf("Port = %d, want 3000", cfg.Port)
	}
	if cfg.DatabaseURL != "postgres://custom:5432/bell" {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, "postgres://custom:5432/bell")
	}
	if cfg.RedisURL != "redis://custom:6379" {
		t.Errorf("RedisURL = %q, want %q", cfg.RedisURL, "redis://custom:6379")
	}
	if cfg.KratosPublicURL != "http://custom:4433" {
		t.Errorf("KratosPublicURL = %q, want %q", cfg.KratosPublicURL, "http://custom:4433")
	}
	if cfg.KratosAdminURL != "http://custom:4434" {
		t.Errorf("KratosAdminURL = %q, want %q", cfg.KratosAdminURL, "http://custom:4434")
	}
	if cfg.ImageStoragePath != "/tmp/images" {
		t.Errorf("ImageStoragePath = %q, want %q", cfg.ImageStoragePath, "/tmp/images")
	}
	if cfg.TownName != "Springfield" {
		t.Errorf("TownName = %q, want %q", cfg.TownName, "Springfield")
	}
}

// validEnv is a production-shaped environment: every variable set to something
// a real deployment would actually use.
func validEnv() map[string]string {
	return map[string]string{
		"PORT":               "8080",
		"BELL_ENV":           "production",
		"DATABASE_URL":       "postgres://bell:secret@db:5432/bell",
		"REDIS_URL":          "redis://cache:6379",
		"KRATOS_PUBLIC_URL":  "http://kratos:4433",
		"KRATOS_ADMIN_URL":   "http://kratos:4434",
		"IMAGE_STORAGE_PATH": "/storage/the-bell/images",
		"TOWN_NAME":          "Bellville",
	}
}

// applyEnv sets every entry, treating "" as "remove this variable" — env
// parsing falls back to a field's envDefault when a variable is present but
// empty, so unsetting is the only way to express absence.
func applyEnv(t *testing.T, values map[string]string) {
	t.Helper()
	for key, value := range values {
		if value == "" {
			unset(t, key)
			continue
		}
		t.Setenv(key, value)
	}
}

// The whole config, end to end through Load, in the shape a real deployment
// has. The rejection tests below are only meaningful if the accepted case is
// pinned too — a validator that rejects everything would pass all of them.
func TestLoad_AcceptsProductionShapedEnvironment(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string]string
	}{
		{
			name:      "fully populated",
			overrides: nil,
		},
		{
			// Running without Redis is a documented degraded mode, so an unset
			// REDIS_URL must stay valid rather than becoming a startup error.
			name:      "no redis configured",
			overrides: map[string]string{"REDIS_URL": ""},
		},
		{
			name:      "development environment",
			overrides: map[string]string{"BELL_ENV": "development"},
		},
		{
			name:      "defaults for everything optional",
			overrides: map[string]string{"PORT": "", "BELL_ENV": "", "REDIS_URL": "", "IMAGE_STORAGE_PATH": "", "TOWN_NAME": ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyEnv(t, validEnv())
			applyEnv(t, tt.overrides)

			if _, err := config.Load(); err != nil {
				t.Fatalf("Load() rejected a usable environment: %v", err)
			}
		})
	}
}

// Each rejection must name the variable at fault. Producing a better message
// than the downstream crash is the entire point of validating at load.
func TestLoad_RejectsUnusableValues(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		wantVar string
	}{
		{"port zero", "PORT", "0", "PORT"},
		{"port above the 16-bit range", "PORT", "70000", "PORT"},
		{"kratos public url with no scheme", "KRATOS_PUBLIC_URL", "kratos:4433", "KRATOS_PUBLIC_URL"},
		{"unparseable kratos admin url", "KRATOS_ADMIN_URL", "http://[::1]:namedport", "KRATOS_ADMIN_URL"},
		{"kratos admin url with no host", "KRATOS_ADMIN_URL", "/kratos", "KRATOS_ADMIN_URL"},
		{"database url that is not a dsn", "DATABASE_URL", "not a dsn", "DATABASE_URL"},
		{"redis url with the wrong scheme", "REDIS_URL", "http://cache:6379", "REDIS_URL"},
		{"relative image storage path", "IMAGE_STORAGE_PATH", "storage/images", "IMAGE_STORAGE_PATH"},
		{"image storage path at the filesystem root", "IMAGE_STORAGE_PATH", "/", "IMAGE_STORAGE_PATH"},
		{"image storage path at a system directory", "IMAGE_STORAGE_PATH", "/etc", "IMAGE_STORAGE_PATH"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyEnv(t, validEnv())
			t.Setenv(tt.key, tt.value)

			_, err := config.Load()
			if err == nil {
				t.Fatalf("Load() accepted %s=%q, want an error", tt.key, tt.value)
			}
			if !strings.Contains(err.Error(), tt.wantVar) {
				t.Errorf("error %q does not name %s", err, tt.wantVar)
			}
		})
	}
}

// A KRATOS_PUBLIC_URL the proxy cannot use must stop the process. Starting
// anyway yields an instance that passes /healthz and serves the SPA while
// login, registration and recovery are all impossible — the worst kind of
// failure, because every automated check reports it healthy.
func TestLoad_RejectsUnusableKratosPublicURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"unparseable", "http://[::1]:namedport"},
		{"control characters", "http://kratos:4433/\x7f"},
		{"no scheme", "kratos:4433"},
		{"path only", "/kratos"},
		{"scheme but no host", "http://"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRequired(t)
			t.Setenv("KRATOS_PUBLIC_URL", tt.url)

			_, err := config.Load()
			if err == nil {
				t.Fatalf("Load() accepted KRATOS_PUBLIC_URL=%q, want an error", tt.url)
			}
			// The operator has to be able to tell which variable to fix.
			if !strings.Contains(err.Error(), "KRATOS_PUBLIC_URL") {
				t.Errorf("error %q does not name the offending variable", err)
			}
		})
	}
}

func TestLoad_AcceptsUsableKratosPublicURL(t *testing.T) {
	for _, u := range []string{
		"http://kratos:4433",
		"https://auth.example.com",
		"http://127.0.0.1:4433/",
	} {
		t.Run(u, func(t *testing.T) {
			setRequired(t)
			t.Setenv("KRATOS_PUBLIC_URL", u)

			if _, err := config.Load(); err != nil {
				t.Errorf("Load() rejected %q: %v", u, err)
			}
		})
	}
}

func TestLoad_InvalidPort(t *testing.T) {
	setRequired(t)
	t.Setenv("PORT", "notanumber")

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for invalid PORT, got nil")
	}
}

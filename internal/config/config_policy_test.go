package config

import (
	"strings"
	"testing"
)

// validConfig is a production-shaped Config: every field set to something a
// real deployment would use. Cases below mutate exactly one field, so any
// failure names the field under test rather than a stale neighbour.
func validConfig() Config {
	return Config{
		Port:             8080,
		Env:              "production",
		DatabaseURL:      "postgres://bell:secret@db:5432/bell",
		RedisURL:         "redis://cache:6379",
		KratosPublicURL:  "http://kratos:4433",
		KratosAdminURL:   "http://kratos:4434",
		ImageStoragePath: "/storage/the-bell/images",
		TownName:         "Bellville",
	}
}

// Each rejection has to name the variable it is about. The entire reason for
// validating here rather than letting the process die downstream is that
// "PORT must be between 1 and 65535" is actionable and "listen tcp: address
// 70000: invalid port" arriving three seconds later is not.
func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantVar string // "" means the config must be accepted
	}{
		{
			name:   "production-shaped config is accepted",
			mutate: func(*Config) {},
		},
		{
			name:   "redis is optional, so unset stays valid",
			mutate: func(c *Config) { c.RedisURL = "" },
		},

		{
			name:    "port zero asks the kernel for any free port",
			mutate:  func(c *Config) { c.Port = 0 },
			wantVar: "PORT",
		},
		{
			name:    "port above the 16-bit range",
			mutate:  func(c *Config) { c.Port = 70000 },
			wantVar: "PORT",
		},
		{
			name:    "negative port",
			mutate:  func(c *Config) { c.Port = -1 },
			wantVar: "PORT",
		},
		{
			name:   "port at the top of the range is fine",
			mutate: func(c *Config) { c.Port = 65535 },
		},

		{
			name:    "unparseable kratos public url",
			mutate:  func(c *Config) { c.KratosPublicURL = "http://[::1]:namedport" },
			wantVar: "KRATOS_PUBLIC_URL",
		},
		{
			name:    "kratos public url with no scheme",
			mutate:  func(c *Config) { c.KratosPublicURL = "kratos:4433" },
			wantVar: "KRATOS_PUBLIC_URL",
		},
		{
			name:    "unparseable kratos admin url",
			mutate:  func(c *Config) { c.KratosAdminURL = "http://[::1]:namedport" },
			wantVar: "KRATOS_ADMIN_URL",
		},
		{
			name:    "kratos admin url with no host",
			mutate:  func(c *Config) { c.KratosAdminURL = "/kratos" },
			wantVar: "KRATOS_ADMIN_URL",
		},

		{
			name:    "database url that is not a dsn at all",
			mutate:  func(c *Config) { c.DatabaseURL = "not a dsn" },
			wantVar: "DATABASE_URL",
		},
		{
			name:    "empty database url",
			mutate:  func(c *Config) { c.DatabaseURL = "" },
			wantVar: "DATABASE_URL",
		},
		{
			name:   "keyword/value dsn is a legitimate postgres form",
			mutate: func(c *Config) { c.DatabaseURL = "host=db port=5432 user=bell dbname=bell" },
		},
		{
			// The shape docker-compose.yml actually ships, query string and all.
			name: "deployment dsn with sslmode",
			mutate: func(c *Config) {
				c.DatabaseURL = "postgres://belluser:hunter2@bell-postgres:5432/bell?sslmode=disable"
			},
		},

		{
			name:    "redis url with the wrong scheme",
			mutate:  func(c *Config) { c.RedisURL = "http://cache:6379" },
			wantVar: "REDIS_URL",
		},
		{
			name:   "rediss is a legitimate redis scheme",
			mutate: func(c *Config) { c.RedisURL = "rediss://cache:6380" },
		},

		{
			name:    "empty image storage path",
			mutate:  func(c *Config) { c.ImageStoragePath = "" },
			wantVar: "IMAGE_STORAGE_PATH",
		},
		{
			name:    "whitespace-only image storage path",
			mutate:  func(c *Config) { c.ImageStoragePath = "   " },
			wantVar: "IMAGE_STORAGE_PATH",
		},
		{
			name:    "relative image storage path",
			mutate:  func(c *Config) { c.ImageStoragePath = "storage/images" },
			wantVar: "IMAGE_STORAGE_PATH",
		},
		{
			name:    "filesystem root",
			mutate:  func(c *Config) { c.ImageStoragePath = "/" },
			wantVar: "IMAGE_STORAGE_PATH",
		},
		{
			name:    "system directory",
			mutate:  func(c *Config) { c.ImageStoragePath = "/etc" },
			wantVar: "IMAGE_STORAGE_PATH",
		},
		{
			name:    "system directory reached with a trailing slash",
			mutate:  func(c *Config) { c.ImageStoragePath = "/etc/" },
			wantVar: "IMAGE_STORAGE_PATH",
		},
		{
			name:    "system directory reached with a dot element",
			mutate:  func(c *Config) { c.ImageStoragePath = "/etc/." },
			wantVar: "IMAGE_STORAGE_PATH",
		},
		{
			name:    "home directory root",
			mutate:  func(c *Config) { c.ImageStoragePath = "/home" },
			wantVar: "IMAGE_STORAGE_PATH",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(&cfg)

			err := cfg.validate()

			if tt.wantVar == "" {
				if err != nil {
					t.Fatalf("validate() rejected a usable config: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validate() accepted the config, want an error about %s", tt.wantVar)
			}
			if !strings.Contains(err.Error(), tt.wantVar) {
				t.Errorf("error %q does not name %s", err, tt.wantVar)
			}
		})
	}
}

// The exact-match rule is what keeps the system-directory check from breaking
// real deployments: the hazard is serving a whole system tree, not living
// anywhere underneath one. /var/lib/bell/images is an ordinary place to put
// application data.
func TestValidateImageStoragePath_AcceptsSubdirectoriesOfSystemDirs(t *testing.T) {
	for _, path := range []string{
		"/var/lib/the-bell/images",
		"/etc/the-bell/images",
		"/home/bell/images",
		"/usr/share/the-bell/images",
		"/storage/the-bell/images/", // a trailing slash is normal in env vars
		"/srv/uploads",
		"/data/images", // what deploy/docker-compose.yml ships
	} {
		t.Run(path, func(t *testing.T) {
			if err := validateImageStoragePath(path); err != nil {
				t.Errorf("rejected a legitimate layout: %v", err)
			}
		})
	}
}

func TestAbsoluteURL(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"http", "http://kratos:4433", false},
		{"https with a path", "https://auth.example.com/kratos", false},
		{"trailing slash", "http://127.0.0.1:4433/", false},
		{"unparseable", "http://[::1]:namedport", true},
		// The realistic typo: it parses, with "kratos" read as the scheme.
		{"host and port with no scheme", "kratos:4433", true},
		{"path only", "/kratos", true},
		{"scheme with no host", "http://", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := absoluteURL("TEST_URL", tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("absoluteURL(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "TEST_URL") {
				t.Errorf("error %q does not name the variable", err)
			}
		})
	}
}

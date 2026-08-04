package config

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Config struct {
	Port             int    `env:"PORT" envDefault:"8080"`
	Env              string `env:"BELL_ENV" envDefault:"production"`
	DatabaseURL      string `env:"DATABASE_URL,required"`
	RedisURL         string `env:"REDIS_URL" envDefault:""`
	KratosPublicURL  string `env:"KRATOS_PUBLIC_URL,required"`
	KratosAdminURL   string `env:"KRATOS_ADMIN_URL,required"`
	ImageStoragePath string `env:"IMAGE_STORAGE_PATH" envDefault:"/storage/the-bell/images"`
	TownName         string `env:"TOWN_NAME" envDefault:"My Town"`
}

// IsDev reports whether the server is running against a local development
// stack, where relaxations like plain-HTTP session cookies are acceptable.
//
// It is an allow-list on purpose: an unset, empty or unrecognised BELL_ENV
// means production. A typo must never be the thing that silently downgrades a
// real user's cookie security.
func (c Config) IsDev() bool {
	switch strings.ToLower(strings.TrimSpace(c.Env)) {
	case "dev", "development":
		return true
	default:
		return false
	}
}

// absoluteURL checks that value is a URL the process can actually dial.
//
// url.Parse on its own is a weak gate: it accepts almost any relative
// reference, so "kratos:4433" and "/kratos" both parse cleanly and both point
// nowhere. A scheme and a host are what make the value usable.
func absoluteURL(name, value string) error {
	u, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("%s is not a valid URL: %w", name, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s must be an absolute URL with a scheme and host, got %q", name, value)
	}
	return nil
}

// systemDirs are directories where serving the entire tree is never what an
// operator meant. Matching is exact: "/etc" is refused, "/etc/bell/images" is
// fine, because the hazard is the tree itself and not the prefix.
var systemDirs = map[string]bool{
	"/": true, "/bin": true, "/boot": true, "/dev": true, "/etc": true,
	"/home": true, "/lib": true, "/proc": true, "/root": true, "/run": true,
	"/sbin": true, "/sys": true, "/usr": true, "/var": true,
}

// validateImageStoragePath rejects values that would put something other than
// uploads behind /uploads/*.
//
// That route serves this directory with http.FileServer, so every regular file
// underneath it is readable by anyone who can name it — fileOnlyFS suppresses
// the directory listing, not the reads. The checks are structural on purpose:
//
//   - Empty would leave http.Dir("") resolving against the process working
//     directory, which is not the same place in a container as it is locally.
//   - A relative path is wrong for the same reason.
//   - The exact-match list catches the handful of cases where the mistake is
//     catastrophic rather than merely wrong.
//
// It deliberately stops there. Whether /srv/data belongs to this app or is
// shared with another is a question about one deployment's layout that config
// cannot answer, and a longer denylist would buy false confidence rather than
// safety — it would still pass anything unlisted. The real containment is that
// nothing but uploads is ever written here.
func validateImageStoragePath(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("IMAGE_STORAGE_PATH must not be empty")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("IMAGE_STORAGE_PATH must be an absolute path, got %q", path)
	}
	// Clean first so a trailing slash or a "." element cannot walk past the
	// check — "/etc/" and "/etc/." both name /etc.
	if systemDirs[filepath.Clean(path)] {
		return fmt.Errorf("IMAGE_STORAGE_PATH must not be a system directory, got %q", path)
	}
	return nil
}

// validate rejects values that survive env parsing but cannot be used.
//
// Every check here corresponds to a failure that is otherwise confusing or
// arrives too late to be actionable. KRATOS_PUBLIC_URL is the clearest case:
// it is the browser's only route to Kratos, so a bad value produces an
// instance that serves the SPA shell and answers /healthz while nobody can
// authenticate — the health check stays green on a product that is unusable.
// Refusing to start is the policy the `required` tag already applies to a
// missing value; these are the values that are present but unusable.
//
// DATABASE_URL and REDIS_URL are handed to the very parsers that will consume
// them later (pgxpool.ParseConfig, redis.ParseURL) so this check cannot drift
// from what actually happens at connect time.
func (c Config) validate() error {
	// Port 0 is legal at the syscall layer — it asks the kernel for any free
	// port — which makes it a bad default for a server nobody could then find.
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("PORT must be between 1 and 65535, got %d", c.Port)
	}
	if err := absoluteURL("KRATOS_PUBLIC_URL", c.KratosPublicURL); err != nil {
		return err
	}
	if err := absoluteURL("KRATOS_ADMIN_URL", c.KratosAdminURL); err != nil {
		return err
	}
	// Checked before parsing because pgxpool.ParseConfig("") succeeds: an empty
	// DSN means "fall back to libpq defaults and the PG* environment", so an
	// unset DATABASE_URL would quietly aim the pool at a local socket as the
	// current user instead of failing.
	if strings.TrimSpace(c.DatabaseURL) == "" {
		return fmt.Errorf("DATABASE_URL must not be empty")
	}
	if _, err := pgxpool.ParseConfig(c.DatabaseURL); err != nil {
		return fmt.Errorf("DATABASE_URL is not a usable postgres DSN: %w", err)
	}
	// Redis is optional: running without it is a documented degraded mode, so
	// unset must stay valid. Only a value that was actually supplied is checked.
	if c.RedisURL != "" {
		if _, err := redis.ParseURL(c.RedisURL); err != nil {
			return fmt.Errorf("REDIS_URL is not a usable redis URL: %w", err)
		}
	}
	return validateImageStoragePath(c.ImageStoragePath)
}

func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

package config

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

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

	// PublicURL is the address residents type — the origin invitation links are
	// built on.
	//
	// Optional, and its absence is handled rather than defaulted. The
	// application has no other way to learn its own public address: Kratos is
	// told one, the reverse proxy knows one, and the Go process sees neither.
	// The obvious substitute, the request's Host header, is attacker-controlled
	// — one request with a forged Host would produce an invitation link
	// pointing at somebody else's site — so when this is empty the API returns
	// invitation links as site-relative paths and the frontend absolutizes them
	// against the origin the member is already on. Only the emailed copy needs
	// a real value, which is why the deploy docs pair this with SMTP.
	PublicURL string `env:"PUBLIC_URL" envDefault:""`

	// SMTPConnectionURI and SMTPFromAddress configure the relay The Bell sends
	// invitation mail through.
	//
	// Both are optional and empty means sending is off: invitations are still
	// created and still work, the response says email_sent:false, and the
	// member passes the link on themselves. That is the right default for a
	// town with no relay, and it is why this is not `required` — a missing
	// SMTP setting must not stop the server booting.
	//
	// The URI is Kratos's courier shape (smtp[s]://user:pass@host:port/?...),
	// deliberately, so both composes can feed this and COURIER_SMTP_* from one
	// variable and an operator configures their relay once. See internal/mail.
	SMTPConnectionURI string `env:"SMTP_CONNECTION_URI" envDefault:""`
	SMTPFromAddress   string `env:"SMTP_FROM_ADDRESS" envDefault:""`

	// TrustSweepInterval is how often the Redis-backed trust worker puts every
	// active user back through the trust calculation. It has no effect without
	// REDIS_URL, where `bell check-roles` is the only thing that recalculates.
	//
	// Daily suits inputs that move with the calendar — the shortest penalty
	// decay window is 90 days and tenure resolves to a day — so shortening it
	// mostly costs work. Lengthening it is the change with teeth: penalties
	// linger past the point the moderator who applied them intended, and role
	// checks judge scores that are as stale as this interval.
	TrustSweepInterval time.Duration `env:"TRUST_SWEEP_INTERVAL" envDefault:"24h"`

	// TrustedProxies lists the peers whose X-Forwarded-For header is believed,
	// as a comma-separated set of IP addresses and CIDR blocks.
	//
	// It is what makes the registration rate limit per-resident rather than
	// per-deployment. Empty — the default — means every request is attributed
	// to its TCP peer, which behind Traefik is Traefik: correct, in that nobody
	// can forge their way into a fresh bucket, but it puts the whole town in one
	// bucket. Set it to the network the proxy connects from.
	//
	// The default is empty rather than "trust the usual private ranges" because
	// the wrong direction of error is unbounded: a deployment that reaches the
	// app directly from the internet, with this filled in optimistically, would
	// let any caller set their own key and lift the limit entirely.
	TrustedProxies string `env:"TRUSTED_PROXIES" envDefault:""`

	// RequireVerifiedEmail gates participation on a verified Kratos address.
	//
	// Off by default, and the default is load-bearing: verification mail goes
	// out through the Kratos courier, so on a town with no working SMTP relay
	// turning this on locks out every resident, including the council that would
	// have to turn it back off. See the admin guide before enabling.
	RequireVerifiedEmail bool `env:"REQUIRE_VERIFIED_EMAIL" envDefault:"false"`
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
// storage.LocalStorage is rooted here and that route serves through it, so
// every regular file directly underneath is readable by anyone who can name it.
// Refusing a listing does not change that — it withholds the names, not the
// reads. The checks are structural on purpose:
//
//   - Empty would leave the storage root resolving against the process working
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
//
// Every check runs and the failures are joined, rather than returning at the
// first one. A .env copied from the wrong environment is usually wrong in
// several places at once, and one-error-per-restart makes the operator
// discover them one deploy at a time — each fix revealing the next problem.
// errors.Join renders them one per line, and each message still names its
// variable, so the aggregate reads as a list of things to fix.
func (c Config) validate() error {
	var errs []error

	// Port 0 is legal at the syscall layer — it asks the kernel for any free
	// port — which makes it a bad default for a server nobody could then find.
	if c.Port < 1 || c.Port > 65535 {
		errs = append(errs, fmt.Errorf("PORT must be between 1 and 65535, got %d", c.Port))
	}
	errs = append(errs, absoluteURL("KRATOS_PUBLIC_URL", c.KratosPublicURL))
	errs = append(errs, absoluteURL("KRATOS_ADMIN_URL", c.KratosAdminURL))

	// Checked before parsing because pgxpool.ParseConfig("") succeeds: an empty
	// DSN means "fall back to libpq defaults and the PG* environment", so an
	// unset DATABASE_URL would quietly aim the pool at a local socket as the
	// current user instead of failing.
	//
	// The two DATABASE_URL checks stay mutually exclusive: an empty value has
	// one thing wrong with it, and saying so twice would be noise rather than
	// the extra information joining is for.
	if strings.TrimSpace(c.DatabaseURL) == "" {
		errs = append(errs, fmt.Errorf("DATABASE_URL must not be empty"))
	} else if _, err := pgxpool.ParseConfig(c.DatabaseURL); err != nil {
		errs = append(errs, fmt.Errorf("DATABASE_URL is not a usable postgres DSN: %w", err))
	}

	// Redis is optional: running without it is a documented degraded mode, so
	// unset must stay valid. Only a value that was actually supplied is checked.
	if c.RedisURL != "" {
		if _, err := redis.ParseURL(c.RedisURL); err != nil {
			errs = append(errs, fmt.Errorf("REDIS_URL is not a usable redis URL: %w", err))
		}
	}

	errs = append(errs, validateImageStoragePath(c.ImageStoragePath))

	// Only a value that was actually supplied is checked, because empty means
	// "links are relative", which is a supported configuration.
	if strings.TrimSpace(c.PublicURL) != "" {
		errs = append(errs, absoluteURL("PUBLIC_URL", c.PublicURL))
	}

	// A relay with nobody to send as is not a working relay: the SMTP
	// conversation opens with MAIL FROM, so this would fail at the first
	// message rather than at boot — which is to say, on the first member who
	// tried to invite somebody. Caught here instead. The reverse (a from
	// address with no relay) is harmless and left alone: sending is simply off.
	if strings.TrimSpace(c.SMTPConnectionURI) != "" && strings.TrimSpace(c.SMTPFromAddress) == "" {
		errs = append(errs, fmt.Errorf("SMTP_FROM_ADDRESS must be set when SMTP_CONNECTION_URI is"))
	}

	// Zero and negative are both rejected here rather than left to
	// TrustWorker.SetSweepInterval, which ignores them. Ignoring is the right
	// behaviour for a library guard but the wrong one for an operator: they
	// would get the 24h default and a single warning line, having asked for
	// something else. A duration that does not parse at all is caught earlier,
	// by env.Parse.
	if c.TrustSweepInterval <= 0 {
		errs = append(errs, fmt.Errorf("TRUST_SWEEP_INTERVAL must be a positive duration, got %s", c.TrustSweepInterval))
	}

	// errors.Join drops nils and returns nil when every entry is nil, so the
	// unconditional appends above cost nothing on a valid config.
	return errors.Join(errs...)
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

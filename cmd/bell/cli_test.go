package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// Pressing Enter at "Enter town name [My Town]" must mean "use My Town".
// Returning "" would reach BootstrapService.Setup, which reads an empty name as
// "leave it unset" — the town would end up unnamed after the operator was shown
// a name and accepted it.
func TestResolveTownName(t *testing.T) {
	tests := []struct {
		name       string
		flagValue  string
		prompted   string
		cfgDefault string
		want       string
	}{
		{
			name:       "empty prompt answer falls back to the displayed default",
			flagValue:  "",
			prompted:   "",
			cfgDefault: "My Town",
			want:       "My Town",
		},
		{
			name:       "whitespace-only answer is still an empty answer",
			flagValue:  "",
			prompted:   "   \t ",
			cfgDefault: "My Town",
			want:       "My Town",
		},
		{
			name:       "typed answer wins over the default",
			flagValue:  "",
			prompted:   "Springfield",
			cfgDefault: "My Town",
			want:       "Springfield",
		},
		{
			name:       "typed answer is trimmed",
			flagValue:  "",
			prompted:   "  Springfield  ",
			cfgDefault: "My Town",
			want:       "Springfield",
		},
		{
			name:       "flag wins over everything",
			flagValue:  "Shelbyville",
			prompted:   "Springfield",
			cfgDefault: "My Town",
			want:       "Shelbyville",
		},
		{
			name:       "flag is used when there was no prompt",
			flagValue:  "Shelbyville",
			prompted:   "",
			cfgDefault: "My Town",
			want:       "Shelbyville",
		},
		{
			// Only reachable if TOWN_NAME is explicitly set to empty, since the
			// config default is "My Town". Setup then leaves the name unset,
			// which is the honest outcome when nobody supplied one.
			name:       "everything empty yields empty",
			flagValue:  "",
			prompted:   "",
			cfgDefault: "",
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveTownName(tt.flagValue, tt.prompted, tt.cfgDefault)
			if got != tt.want {
				t.Errorf("resolveTownName(%q, %q, %q) = %q, want %q",
					tt.flagValue, tt.prompted, tt.cfgDefault, got, tt.want)
			}
		})
	}
}

// The regression this pins: the old code assigned to *townName only when the
// input was non-empty, so an accepted default arrived at Setup as "".
func TestResolveTownName_AcceptedDefaultIsNeverEmpty(t *testing.T) {
	if got := resolveTownName("", "", "My Town"); got == "" {
		t.Error("accepting the offered default produced an empty town name")
	}
}

func TestKratosHealthURL(t *testing.T) {
	tests := []struct {
		name     string
		adminURL string
		want     string
	}{
		{
			name:     "no trailing slash",
			adminURL: "http://kratos:4434",
			want:     "http://kratos:4434/health/alive",
		},
		{
			// Kratos does not serve "//health/alive", so the slash has to go.
			name:     "trailing slash is trimmed",
			adminURL: "http://kratos:4434/",
			want:     "http://kratos:4434/health/alive",
		},
		{
			name:     "several trailing slashes",
			adminURL: "http://kratos:4434///",
			want:     "http://kratos:4434/health/alive",
		},
		{
			name:     "path prefix is preserved",
			adminURL: "https://auth.example.com/kratos",
			want:     "https://auth.example.com/kratos/health/alive",
		},
		{
			name:     "path prefix with trailing slash",
			adminURL: "https://auth.example.com/kratos/",
			want:     "https://auth.example.com/kratos/health/alive",
		},
		{
			name:     "empty input still produces a relative path",
			adminURL: "",
			want:     "/health/alive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := kratosHealthURL(tt.adminURL); got != tt.want {
				t.Errorf("kratosHealthURL(%q) = %q, want %q", tt.adminURL, got, tt.want)
			}
		})
	}
}

func TestDialAddr(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{
			name: "explicit port",
			dsn:  "postgres://bell:secret@db.example.com:5433/bell",
			want: "db.example.com:5433",
		},
		{
			// pgx fills in 5432 when the DSN omits it.
			name: "default port is supplied",
			dsn:  "postgres://bell:secret@db.example.com/bell",
			want: "db.example.com:5432",
		},
		{
			name: "postgresql scheme",
			dsn:  "postgresql://bell@localhost:5432/bell",
			want: "localhost:5432",
		},
		{
			name: "ipv6 host is bracketed",
			dsn:  "postgres://bell@[::1]:5432/bell",
			want: "[::1]:5432",
		},
		{
			name: "query parameters do not affect the address",
			dsn:  "postgres://bell@db:5432/bell?sslmode=disable",
			want: "db:5432",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := dialAddr(tt.dsn)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("dialAddr(%q) = %q, want %q", tt.dsn, got, tt.want)
			}
		})
	}
}

func TestDialAddr_Rejects(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
	}{
		{
			// pgx defaults an empty DSN to the unix socket dir, which would
			// otherwise be probed as the TCP address "/tmp:5432".
			name: "empty defaults to a unix socket, not a TCP host",
			dsn:  "",
		},
		{"not a DSN at all", "this is not a dsn"},
		{"non-numeric port", "postgres://bell@db:notaport/bell"},
		{"unknown scheme", "mysql://bell@db:3306/bell"},
		{"explicit unix socket host", "postgres:///bell?host=/var/run/postgresql"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, err := dialAddr(tt.dsn)
			if err == nil {
				t.Fatalf("expected an error, got address %q", addr)
			}
		})
	}
}

// CREATE DATABASE cannot run from inside the database being created, so the
// admin connection is redirected to "postgres" while keeping every other part
// of the DSN — credentials, host, port and options — intact.
func TestAdminDSN(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{
			name: "database name is replaced",
			dsn:  "postgres://bell:secret@db:5432/bell",
			want: "postgres://bell:secret@db:5432/postgres",
		},
		{
			name: "credentials and options survive",
			dsn:  "postgres://bell:secret@db:5432/thebell?sslmode=disable",
			want: "postgres://bell:secret@db:5432/postgres?sslmode=disable",
		},
		{
			name: "already pointing at postgres",
			dsn:  "postgres://bell@db:5432/postgres",
			want: "postgres://bell@db:5432/postgres",
		},
		{
			name: "no database in the path",
			dsn:  "postgres://bell@db:5432",
			want: "postgres://bell@db:5432/postgres",
		},
		{
			name: "postgresql scheme is preserved",
			dsn:  "postgresql://bell@db:5432/bell",
			want: "postgresql://bell@db:5432/postgres",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := adminDSN(tt.dsn)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("adminDSN(%q) = %q, want %q", tt.dsn, got, tt.want)
			}
		})
	}
}

func TestAdminDSN_Rejects(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
	}{
		{"empty", ""},
		{"unparseable", "postgres://bell@db:%zz/bell"},
		{"keyword DSN form is not a URL", "host=localhost port=5432 dbname=bell"},
		{"wrong scheme", "mysql://bell@db:3306/bell"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := adminDSN(tt.dsn)
			if err == nil {
				t.Fatalf("expected an error, got %q", got)
			}
		})
	}
}

// --create-db used to loop over the literals {"bell", "bell_kratos"} while
// connecting per DATABASE_URL, so pointing DATABASE_URL at .../thebell created
// "bell" and then failed to migrate. Both names now come from the DSN.
func TestDatabaseNames(t *testing.T) {
	tests := []struct {
		name        string
		dsn         string
		wantPrimary string
		wantKratos  string
	}{
		{
			name:        "conventional bell database",
			dsn:         "postgres://bell@db:5432/bell",
			wantPrimary: "bell",
			wantKratos:  "bell_kratos",
		},
		{
			name:        "custom database name",
			dsn:         "postgres://bell@db:5432/thebell",
			wantPrimary: "thebell",
			wantKratos:  "thebell_kratos",
		},
		{
			name:        "name with underscores and digits",
			dsn:         "postgres://bell@db:5432/town_bell_2",
			wantPrimary: "town_bell_2",
			wantKratos:  "town_bell_2_kratos",
		},
		{
			name:        "query parameters are ignored",
			dsn:         "postgres://bell@db:5432/thebell?sslmode=require&pool_max_conns=10",
			wantPrimary: "thebell",
			wantKratos:  "thebell_kratos",
		},
		{
			name:        "percent-encoded name is decoded",
			dsn:         "postgres://bell@db:5432/my%20town",
			wantPrimary: "my town",
			wantKratos:  "my town_kratos",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			primary, kratos, err := databaseNames(tt.dsn)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if primary != tt.wantPrimary {
				t.Errorf("primary = %q, want %q", primary, tt.wantPrimary)
			}
			if kratos != tt.wantKratos {
				t.Errorf("kratos = %q, want %q", kratos, tt.wantKratos)
			}
		})
	}
}

func TestDatabaseNames_Rejects(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
	}{
		{"no database in the path", "postgres://bell@db:5432"},
		{"bare slash is no database", "postgres://bell@db:5432/"},
		{"keyword DSN form", "host=localhost dbname=bell"},
		{"wrong scheme", "mysql://bell@db:3306/bell"},
		{"empty", ""},
		{
			// Postgres truncates identifiers at 63 bytes, so a long primary name
			// would silently collide with another town's Kratos database.
			name: "name too long once _kratos is appended",
			dsn:  "postgres://bell@db:5432/" + strings.Repeat("a", 60),
		},
		{
			// A percent-encoded NUL survives url.Parse into the decoded path.
			name: "percent-encoded NUL in the name",
			dsn:  "postgres://bell@db:5432/be%00ll",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			primary, kratos, err := databaseNames(tt.dsn)
			if err == nil {
				t.Fatalf("expected an error, got %q and %q", primary, kratos)
			}
		})
	}
}

// A name at the limit must still be accepted — the check is a guard, not an
// off-by-one that rejects valid configurations.
func TestDatabaseNames_AcceptsNameAtTheLimit(t *testing.T) {
	primary := strings.Repeat("a", 63-len("_kratos"))

	got, kratos, err := databaseNames("postgres://bell@db:5432/" + primary)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != primary {
		t.Errorf("primary = %q, want %q", got, primary)
	}
	if len(kratos) != 63 {
		t.Errorf("len(kratos) = %d, want exactly 63", len(kratos))
	}
}

func TestRun_UsageAndUnknownCommand(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStderr string
	}{
		{
			name:       "no subcommand prints usage",
			args:       []string{"bell"},
			wantCode:   1,
			wantStderr: "usage: bell <command>",
		},
		{
			name:       "unknown subcommand is named in the error",
			args:       []string{"bell", "frobnicate"},
			wantCode:   1,
			wantStderr: "unknown command: frobnicate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := run(tt.args, strings.NewReader(""), &stdout, &stderr)

			if code != tt.wantCode {
				t.Errorf("run() = %d, want %d", code, tt.wantCode)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

// Usage lists every subcommand run actually dispatches. A command added to the
// switch without a usage line would be invisible to operators.
func TestUsageListsEverySubcommand(t *testing.T) {
	for _, cmd := range []string{"serve", "setup", "check-roles", "backfill-display-names"} {
		if !strings.Contains(usage, cmd) {
			t.Errorf("usage does not mention the %q subcommand", cmd)
		}
	}
}

// A mistyped flag must stop the command before it opens a database connection —
// an operator who meant --dry-run and typed --dryrun would otherwise get a live
// run that writes.
func TestRunBackfillDisplayNames_RejectsUnknownFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&stdout, nil))

	code := runBackfillDisplayNames(logger, []string{"--dryrun"}, &stdout, &stderr)

	if code != 1 {
		t.Errorf("runBackfillDisplayNames() = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "dryrun") {
		t.Errorf("stderr = %q, want it to name the unrecognized flag", stderr.String())
	}
}

// Nothing is written to stdout on a usage error: the process is exiting
// non-zero, so the diagnostic belongs on stderr where it can be redirected
// separately from real output.
func TestRun_UsageGoesToStderrOnly(t *testing.T) {
	var stdout, stderr bytes.Buffer

	run([]string{"bell"}, strings.NewReader(""), &stdout, &stderr)

	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want it empty", stdout.String())
	}
	if stderr.Len() == 0 {
		t.Error("stderr is empty, want the usage text")
	}
}

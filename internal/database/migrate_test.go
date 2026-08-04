package database

import (
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/fireynis/the-bell/migrations"
)

// migrationNamePattern is goose's required filename shape: a zero-padded
// version prefix followed by a snake_case description.
var migrationNamePattern = regexp.MustCompile(`^(\d{5})_[a-z0-9]+(?:_[a-z0-9]+)*\.sql$`)

// minMigrationCount is a floor, not an exact count. It exists so that a broken
// //go:embed glob — which yields an empty FS and would otherwise sail through
// every check below — fails loudly. Adding a migration must never require
// touching this; only deliberately removing or squashing migrations does.
const minMigrationCount = 13

func embeddedMigrationNames(t *testing.T) []string {
	t.Helper()

	var found []string
	err := fs.WalkDir(migrations.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking embedded migrations: %v", err)
	}
	sort.Strings(found)
	return found
}

func TestEmbeddedMigrations_AreEmbedded(t *testing.T) {
	found := embeddedMigrationNames(t)

	if len(found) < minMigrationCount {
		t.Fatalf("found %d embedded migrations, want at least %d: %v", len(found), minMigrationCount, found)
	}
}

// Versions must run 1..N with no gaps and no duplicates. A duplicate is the
// realistic mistake here: two branches each add "the next" number, and after a
// merge goose records that version as applied and silently skips the other
// file. A gap means a migration was lost on its way into the tree.
func TestEmbeddedMigrations_AreSequentiallyNumbered(t *testing.T) {
	found := embeddedMigrationNames(t)

	byVersion := make(map[int]string, len(found))
	for _, name := range found {
		match := migrationNamePattern.FindStringSubmatch(name)
		if match == nil {
			t.Errorf("%q does not match goose's NNNNN_snake_case.sql naming", name)
			continue
		}
		version, err := strconv.Atoi(match[1])
		if err != nil {
			t.Errorf("%q: unparsable version prefix: %v", name, err)
			continue
		}
		if existing, duplicate := byVersion[version]; duplicate {
			t.Errorf("version %d claimed twice, by %q and %q", version, existing, name)
			continue
		}
		byVersion[version] = name
	}

	for version := 1; version <= len(byVersion); version++ {
		if _, ok := byVersion[version]; !ok {
			t.Errorf("no migration numbered %05d; versions must run 1..%d with no gaps", version, len(byVersion))
		}
	}
}

// Every embedded file must be something goose can actually run in both
// directions. A file whose Down block is missing or empty applies cleanly and
// then cannot be rolled back — which is only discovered during an incident.
func TestEmbeddedMigrations_HaveUpAndDownBlocks(t *testing.T) {
	for _, name := range embeddedMigrationNames(t) {
		t.Run(name, func(t *testing.T) {
			body, err := fs.ReadFile(migrations.FS, name)
			if err != nil {
				t.Fatalf("reading %q: %v", name, err)
			}

			up, down, err := splitGooseSections(string(body))
			if err != nil {
				t.Fatalf("%v", err)
			}
			if up == "" {
				t.Error("Up block contains no SQL")
			}
			if down == "" {
				t.Error("Down block contains no SQL")
			}
		})
	}
}

// The checks above only earn their keep if they reject a bad file, so exercise
// the detector directly rather than trusting that the current tree is clean.
func TestSplitGooseSections(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantUp   string
		wantDown string
		wantErr  bool
	}{
		{
			name:     "well-formed migration",
			body:     "-- +goose Up\nCREATE TABLE t (id int);\n\n-- +goose Down\nDROP TABLE t;\n",
			wantUp:   "CREATE TABLE t (id int);",
			wantDown: "DROP TABLE t;",
		},
		{
			name:     "leading comments and blank lines are not SQL",
			body:     "-- adds the t table\n-- +goose Up\n\n-- explanatory\nCREATE TABLE t (id int);\n-- +goose Down\nDROP TABLE t;\n",
			wantUp:   "CREATE TABLE t (id int);",
			wantDown: "DROP TABLE t;",
		},
		{
			name:     "empty Down block is reported as empty, not missing",
			body:     "-- +goose Up\nCREATE TABLE t (id int);\n-- +goose Down\n-- irreversible\n",
			wantUp:   "CREATE TABLE t (id int);",
			wantDown: "",
		},
		{
			name:    "missing Down annotation",
			body:    "-- +goose Up\nCREATE TABLE t (id int);\n",
			wantErr: true,
		},
		{
			name:    "missing Up annotation",
			body:    "-- +goose Down\nDROP TABLE t;\n",
			wantErr: true,
		},
		{
			name:    "duplicated Up annotation",
			body:    "-- +goose Up\nCREATE TABLE t (id int);\n-- +goose Up\n-- +goose Down\nDROP TABLE t;\n",
			wantErr: true,
		},
		{
			name:    "Down before Up",
			body:    "-- +goose Down\nDROP TABLE t;\n-- +goose Up\nCREATE TABLE t (id int);\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			up, down, err := splitGooseSections(tt.body)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("splitGooseSections() returned no error, want one")
				}
				return
			}
			if err != nil {
				t.Fatalf("splitGooseSections() error = %v", err)
			}
			if up != tt.wantUp {
				t.Errorf("up = %q, want %q", up, tt.wantUp)
			}
			if down != tt.wantDown {
				t.Errorf("down = %q, want %q", down, tt.wantDown)
			}
		})
	}
}

// splitGooseSections returns the SQL under the Up and Down annotations with
// comments and blank lines stripped, so callers can tell "block is present"
// from "block actually does something". It reports an error unless the file
// carries exactly one Up annotation followed by exactly one Down.
func splitGooseSections(body string) (up, down string, err error) {
	const prefix = "-- +goose "

	upLine, downLine := -1, -1
	var upCount, downCount int
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		directive, ok := strings.CutPrefix(strings.TrimSpace(line), prefix)
		if !ok {
			continue
		}
		switch strings.TrimSpace(directive) {
		case "Up":
			upCount++
			if upLine < 0 {
				upLine = i
			}
		case "Down":
			downCount++
			if downLine < 0 {
				downLine = i
			}
		}
	}

	switch {
	case upCount != 1:
		return "", "", fmt.Errorf("found %d `-- +goose Up` annotations, want exactly 1", upCount)
	case downCount != 1:
		return "", "", fmt.Errorf("found %d `-- +goose Down` annotations, want exactly 1", downCount)
	case upLine > downLine:
		return "", "", fmt.Errorf("`-- +goose Up` appears after `-- +goose Down`; goose applies the file top-down")
	}

	return strippedSQL(lines[upLine+1 : downLine]), strippedSQL(lines[downLine+1:]), nil
}

// strippedSQL joins the lines that carry actual statements, dropping blank
// lines and SQL comments.
func strippedSQL(lines []string) string {
	var kept []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		kept = append(kept, trimmed)
	}
	return strings.Join(kept, "\n")
}

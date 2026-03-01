package database

import (
	"io/fs"
	"testing"

	"github.com/fireynis/the-bell/migrations"
)

func TestEmbeddedMigrations_ContainsExpectedFiles(t *testing.T) {
	expected := []string{
		"00001_enable_extensions.sql",
		"00002_create_users.sql",
		"00003_create_posts.sql",
		"00004_create_reactions.sql",
		"00005_create_moderation.sql",
		"00006_create_reports.sql",
		"00007_create_trust_graph.sql",
		"00008_create_vouches.sql",
		"00009_create_town_config.sql",
	}

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

	if len(found) != len(expected) {
		t.Fatalf("expected %d migration files, got %d: %v", len(expected), len(found), found)
	}

	for i, name := range expected {
		if found[i] != name {
			t.Errorf("migration[%d]: expected %q, got %q", i, name, found[i])
		}
	}
}

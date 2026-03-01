package config_test

import (
	"testing"

	"github.com/fireynis/the-bell/internal/config"
)

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

func TestLoad_InvalidPort(t *testing.T) {
	setRequired(t)
	t.Setenv("PORT", "notanumber")

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for invalid PORT, got nil")
	}
}

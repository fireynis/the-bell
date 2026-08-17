package config_test

import (
	"strings"
	"testing"

	"github.com/fireynis/the-bell/internal/config"
)

// All three are optional, and their absence is a supported configuration:
// invitation links come back relative and nothing is emailed. A server that
// refused to start without them would make invitations a deployment
// prerequisite rather than a feature.
func TestLoad_InviteSettingsDefaultToOff(t *testing.T) {
	setRequired(t)
	unset(t, "PUBLIC_URL")
	unset(t, "SMTP_CONNECTION_URI")
	unset(t, "SMTP_FROM_ADDRESS")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.PublicURL != "" {
		t.Errorf("PublicURL = %q, want empty", cfg.PublicURL)
	}
	if cfg.SMTPConnectionURI != "" {
		t.Errorf("SMTPConnectionURI = %q, want empty", cfg.SMTPConnectionURI)
	}
	if cfg.SMTPFromAddress != "" {
		t.Errorf("SMTPFromAddress = %q, want empty", cfg.SMTPFromAddress)
	}
}

func TestLoad_ReadsTheInviteSettings(t *testing.T) {
	setRequired(t)
	t.Setenv("PUBLIC_URL", "https://bell.example.test")
	t.Setenv("SMTP_CONNECTION_URI", "smtp://mailhog:1025/?disable_starttls=true")
	t.Setenv("SMTP_FROM_ADDRESS", "noreply@bell.example.test")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.PublicURL != "https://bell.example.test" {
		t.Errorf("PublicURL = %q", cfg.PublicURL)
	}
	if cfg.SMTPConnectionURI != "smtp://mailhog:1025/?disable_starttls=true" {
		t.Errorf("SMTPConnectionURI = %q", cfg.SMTPConnectionURI)
	}
	if cfg.SMTPFromAddress != "noreply@bell.example.test" {
		t.Errorf("SMTPFromAddress = %q", cfg.SMTPFromAddress)
	}
}

// A relative or scheme-less PUBLIC_URL would produce invitation links that go
// nowhere from an inbox, which is the one context where the link has to be
// absolute. Caught at boot rather than by the first invitee.
func TestLoad_RejectsAnUnusablePublicURL(t *testing.T) {
	for _, value := range []string{"bell.example.test", "/bell", "://nope"} {
		t.Run(value, func(t *testing.T) {
			setRequired(t)
			t.Setenv("PUBLIC_URL", value)

			if _, err := config.Load(); err == nil {
				t.Errorf("config.Load() accepted PUBLIC_URL=%q", value)
			}
		})
	}
}

// A relay with nobody to send as fails at MAIL FROM — that is, on the first
// member who tried to invite somebody, long after boot.
func TestLoad_RejectsSMTPWithoutAFromAddress(t *testing.T) {
	setRequired(t)
	t.Setenv("SMTP_CONNECTION_URI", "smtp://mailhog:1025/?disable_starttls=true")
	unset(t, "SMTP_FROM_ADDRESS")

	_, err := config.Load()
	if err == nil {
		t.Fatal("config.Load() accepted an SMTP URI with no from address")
	}
	if !strings.Contains(err.Error(), "SMTP_FROM_ADDRESS") {
		t.Errorf("error = %v, want it to name SMTP_FROM_ADDRESS", err)
	}
}

// The reverse is harmless: a from address with no relay just means sending is
// off, which is the default anyway.
func TestLoad_AcceptsAFromAddressWithNoRelay(t *testing.T) {
	setRequired(t)
	unset(t, "SMTP_CONNECTION_URI")
	t.Setenv("SMTP_FROM_ADDRESS", "noreply@bell.example.test")

	if _, err := config.Load(); err != nil {
		t.Errorf("config.Load() = %v, want a from address with no relay to be accepted", err)
	}
}

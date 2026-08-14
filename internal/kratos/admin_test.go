package kratos

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	client "github.com/ory/kratos-client-go"
)

// --- newIdentityBody ---

func TestNewIdentityBody(t *testing.T) {
	body := newIdentityBody("alice@example.com", "Alice", "hunter2")

	if got := body.GetSchemaId(); got != "default" {
		t.Errorf("schema_id = %q, want %q", got, "default")
	}
	if got := body.GetState(); got != "active" {
		t.Errorf("state = %q, want %q — a pending identity cannot sign in", got, "active")
	}

	traits := body.GetTraits()
	if traits["email"] != "alice@example.com" {
		t.Errorf("traits.email = %v, want %q", traits["email"], "alice@example.com")
	}
	// The identity schema's name field is what the frontend renders as the
	// display name; sending it under any other key silently loses it.
	if traits["name"] != "Alice" {
		t.Errorf("traits.name = %v, want %q", traits["name"], "Alice")
	}
	if len(traits) != 2 {
		t.Errorf("traits = %v, want exactly email and name", traits)
	}

	creds, ok := body.GetCredentialsOk()
	if !ok {
		t.Fatal("no credentials set: the identity would be created with no way to sign in")
	}
	cfg := creds.Password.GetConfig()
	if got := cfg.GetPassword(); got != "hunter2" {
		t.Errorf("password = %q, want %q", got, "hunter2")
	}
}

// Empty display names and unusual addresses are the caller's business; the body
// builder must pass them through rather than silently substituting anything.
func TestNewIdentityBody_PassesTraitsThroughUnchanged(t *testing.T) {
	tests := []struct {
		name        string
		email       string
		displayName string
	}{
		{"empty display name", "alice@example.com", ""},
		{"plus addressing", "alice+bell@example.com", "Alice"},
		{"unicode display name", "renée@example.com", "Renée O'Brien"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			traits := newIdentityBody(tt.email, tt.displayName, "pw").GetTraits()
			if traits["email"] != tt.email {
				t.Errorf("traits.email = %v, want %q", traits["email"], tt.email)
			}
			if traits["name"] != tt.displayName {
				t.Errorf("traits.name = %v, want %q", traits["name"], tt.displayName)
			}
		})
	}
}

// --- generatePassword ---

func TestGeneratePassword_Contract(t *testing.T) {
	pw, err := generatePassword()
	if err != nil {
		t.Fatalf("generatePassword: %v", err)
	}

	// 32 random bytes, hex encoded.
	if len(pw) != passwordEntropyBytes*2 {
		t.Errorf("len = %d, want %d", len(pw), passwordEntropyBytes*2)
	}
	if _, err := hex.DecodeString(pw); err != nil {
		t.Errorf("password %q is not hex: %v", pw, err)
	}
	if strings.ToLower(pw) != pw {
		t.Errorf("password %q is not lowercase hex", pw)
	}
}

// Every generated password is a live credential, so two accounts created in the
// same run must never share one.
func TestGeneratePassword_IsNotRepeated(t *testing.T) {
	const runs = 1000

	seen := make(map[string]struct{}, runs)
	for i := 0; i < runs; i++ {
		pw, err := generatePassword()
		if err != nil {
			t.Fatalf("generatePassword: %v", err)
		}
		if _, dup := seen[pw]; dup {
			t.Fatalf("generatePassword repeated %q within %d calls", pw, runs)
		}
		seen[pw] = struct{}{}
	}
}

// The randomness source is a security property, not an implementation detail:
// math/rand is seeded observably and its output would be reproducible by anyone
// who could guess the process start time. Assert on the source itself, because
// no black-box test of the output can tell the two apart.
func TestGeneratePassword_UsesCryptoRand(t *testing.T) {
	src, err := os.ReadFile("admin.go")
	if err != nil {
		t.Fatalf("reading admin.go: %v", err)
	}

	if !strings.Contains(string(src), `"crypto/rand"`) {
		t.Error("admin.go does not import crypto/rand")
	}
	if strings.Contains(string(src), `"math/rand"`) || strings.Contains(string(src), `"math/rand/v2"`) {
		t.Error("admin.go imports math/rand: generated passwords would be predictable")
	}
}

// --- client configuration ---

// The SDK's default client has no timeout at all, so a Kratos that accepts the
// connection and then never answers would hang `bell setup` with no way out but
// Ctrl-C. NewAdminClient builds its configuration through this function, so
// asserting here covers the wiring it actually uses.
func TestNewConfiguration_BoundsEveryRequest(t *testing.T) {
	cfg := newConfiguration("http://kratos:4434", requestTimeout)

	if cfg.HTTPClient == nil {
		t.Fatal("HTTPClient is nil: the SDK would fall back to its untimed default")
	}
	if cfg.HTTPClient.Timeout != requestTimeout {
		t.Errorf("timeout = %v, want %v", cfg.HTTPClient.Timeout, requestTimeout)
	}
	if requestTimeout <= 0 {
		t.Errorf("requestTimeout = %v, want a positive bound", requestTimeout)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].URL != "http://kratos:4434" {
		t.Errorf("servers = %v, want the single configured admin URL", cfg.Servers)
	}
}

// And the timeout has to actually fire — a configured field that the SDK
// ignored would look identical to the assertion above.
func TestAdminClient_GivesUpOnAStalledKratos(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-release
	}))
	defer srv.Close()
	defer close(release)

	// Same wiring as NewAdminClient, with a short budget so the test does not
	// wait out the production one.
	c := &AdminClient{api: client.NewAPIClient(newConfiguration(srv.URL, 100*time.Millisecond)).IdentityAPI}

	done := make(chan error, 1)
	go func() {
		_, _, err := c.CreateIdentity(context.Background(), "alice@example.com", "Alice", "")
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("CreateIdentity succeeded against a server that never responds")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CreateIdentity never returned: the timeout is not being applied")
	}
}

func TestAdminClient_CreateIdentity_Success(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/admin/identities" {
			t.Errorf("path = %s, want /admin/identities", r.URL.Path)
		}

		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)

		traits := body["traits"].(map[string]interface{})
		if traits["email"] != "alice@example.com" {
			t.Errorf("email = %v, want alice@example.com", traits["email"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":         "kratos-identity-123",
			"schema_id":  "default",
			"schema_url": "http://localhost/schemas/default",
			"traits":     map[string]interface{}{"email": "alice@example.com"},
		})
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := NewAdminClient(srv.URL)
	kratosID, password, err := client.CreateIdentity(context.Background(), "alice@example.com", "Alice", "")
	if err != nil {
		t.Fatalf("CreateIdentity() error: %v", err)
	}
	if kratosID != "kratos-identity-123" {
		t.Errorf("kratosID = %q, want %q", kratosID, "kratos-identity-123")
	}
	if password == "" {
		t.Error("expected a generated password, got empty string")
	}
}

// --- DisplayNameFromTraits / IdentityDisplayName ---

// Traits are decoded JSON against a per-deployment schema, so every shape that
// is not a string under `name` has to come back as "" rather than panicking a
// backfill that is walking the whole town.
func TestDisplayNameFromTraits(t *testing.T) {
	tests := []struct {
		name   string
		traits interface{}
		want   string
	}{
		{"name trait", map[string]interface{}{"email": "a@b.c", "name": "Ada Lovelace"}, "Ada Lovelace"},
		{"name is trimmed", map[string]interface{}{"name": "  Ada \n"}, "Ada"},
		{"whitespace-only name", map[string]interface{}{"name": "   "}, ""},
		{"no name key", map[string]interface{}{"email": "a@b.c"}, ""},
		{"name is not a string", map[string]interface{}{"name": 42}, ""},
		{"name is an object", map[string]interface{}{"name": map[string]interface{}{"first": "Ada"}}, ""},
		{"traits are not an object", []interface{}{"Ada"}, ""},
		{"traits are nil", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DisplayNameFromTraits(tt.traits); got != tt.want {
				t.Errorf("DisplayNameFromTraits(%v) = %q, want %q", tt.traits, got, tt.want)
			}
		})
	}
}

func TestAdminClient_IdentityDisplayName_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/admin/identities/kratos-identity-123" {
			t.Errorf("path = %s, want /admin/identities/kratos-identity-123", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":         "kratos-identity-123",
			"schema_id":  "default",
			"schema_url": "http://localhost/schemas/default",
			"traits":     map[string]interface{}{"email": "alice@example.com", "name": "Alice"},
		})
	}))
	defer srv.Close()

	name, err := NewAdminClient(srv.URL).IdentityDisplayName(context.Background(), "kratos-identity-123")
	if err != nil {
		t.Fatalf("IdentityDisplayName() error: %v", err)
	}
	if name != "Alice" {
		t.Errorf("name = %q, want %q", name, "Alice")
	}
}

// An identity that exists but carries no name is not a failure: the caller has
// nothing to write, which is a different outcome from not being able to ask.
func TestAdminClient_IdentityDisplayName_NoTraitIsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":         "kratos-identity-123",
			"schema_id":  "default",
			"schema_url": "http://localhost/schemas/default",
			"traits":     map[string]interface{}{"email": "alice@example.com"},
		})
	}))
	defer srv.Close()

	name, err := NewAdminClient(srv.URL).IdentityDisplayName(context.Background(), "kratos-identity-123")
	if err != nil {
		t.Fatalf("IdentityDisplayName() error: %v", err)
	}
	if name != "" {
		t.Errorf("name = %q, want empty", name)
	}
}

// A deleted identity that a local user still points at must surface as an
// error naming the identity, so the backfill can count and log it.
func TestAdminClient_IdentityDisplayName_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{"message": "Unable to locate the resource"},
		})
	}))
	defer srv.Close()

	_, err := NewAdminClient(srv.URL).IdentityDisplayName(context.Background(), "kratos-gone")
	if err == nil {
		t.Fatal("IdentityDisplayName() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "kratos-gone") {
		t.Errorf("error = %q, want it to name the identity", err)
	}
}

func TestAdminClient_CreateIdentity_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{"message": "identity already exists"},
		})
	}))
	defer srv.Close()

	client := NewAdminClient(srv.URL)
	_, _, err := client.CreateIdentity(context.Background(), "alice@example.com", "Alice", "")
	if err == nil {
		t.Fatal("CreateIdentity() expected error, got nil")
	}
}

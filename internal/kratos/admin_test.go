package kratos

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
	kratosID, err := client.CreateIdentity(context.Background(), "alice@example.com", "Alice", "")
	if err != nil {
		t.Fatalf("CreateIdentity() error: %v", err)
	}
	if kratosID != "kratos-identity-123" {
		t.Errorf("kratosID = %q, want %q", kratosID, "kratos-identity-123")
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
	_, err := client.CreateIdentity(context.Background(), "alice@example.com", "Alice", "")
	if err == nil {
		t.Fatal("CreateIdentity() expected error, got nil")
	}
}

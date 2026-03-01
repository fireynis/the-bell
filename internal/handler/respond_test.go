package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fireynis/the-bell/internal/handler"
)

func TestJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	handler.JSON(rec, http.StatusOK, map[string]string{"key": "value"})

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if body["key"] != "value" {
		t.Errorf("body[key] = %q, want %q", body["key"], "value")
	}
}

func TestJSON_MarshalError(t *testing.T) {
	rec := httptest.NewRecorder()
	// Channels cannot be marshaled to JSON.
	handler.JSON(rec, http.StatusOK, make(chan int))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to unmarshal error body: %v", err)
	}
	if body["error"] != "internal error" {
		t.Errorf("error = %q, want %q", body["error"], "internal error")
	}
}

func TestError(t *testing.T) {
	rec := httptest.NewRecorder()
	handler.Error(rec, http.StatusBadRequest, "bad input")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if body["error"] != "bad input" {
		t.Errorf("error = %q, want %q", body["error"], "bad input")
	}
}

func TestDecode(t *testing.T) {
	t.Run("valid JSON", func(t *testing.T) {
		body := strings.NewReader(`{"name":"test"}`)
		req := httptest.NewRequest(http.MethodPost, "/", body)

		var dst struct {
			Name string `json:"name"`
		}
		if err := handler.Decode(req, &dst); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if dst.Name != "test" {
			t.Errorf("Name = %q, want %q", dst.Name, "test")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		body := strings.NewReader(`{invalid}`)
		req := httptest.NewRequest(http.MethodPost, "/", body)

		var dst struct{}
		if err := handler.Decode(req, &dst); err == nil {
			t.Fatal("Decode() expected error for invalid JSON")
		}
	})

	t.Run("unknown fields rejected", func(t *testing.T) {
		body := strings.NewReader(`{"name":"test","extra":"field"}`)
		req := httptest.NewRequest(http.MethodPost, "/", body)

		var dst struct {
			Name string `json:"name"`
		}
		if err := handler.Decode(req, &dst); err == nil {
			t.Fatal("Decode() expected error for unknown fields")
		}
	})

	t.Run("empty body", func(t *testing.T) {
		body := strings.NewReader("")
		req := httptest.NewRequest(http.MethodPost, "/", body)

		var dst struct{}
		if err := handler.Decode(req, &dst); err == nil {
			t.Fatal("Decode() expected error for empty body")
		}
	})
}

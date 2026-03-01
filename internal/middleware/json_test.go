package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fireynis/the-bell/internal/middleware"
)

func TestContentTypeJSON(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.ContentTypeJSON(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	got := rec.Header().Get("Content-Type")
	if got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
}

func TestContentTypeJSON_HandlerCanOverride(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.ContentTypeJSON(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	got := rec.Header().Get("Content-Type")
	if got != "text/plain" {
		t.Errorf("Content-Type = %q, want %q (handler should be able to override)", got, "text/plain")
	}
}

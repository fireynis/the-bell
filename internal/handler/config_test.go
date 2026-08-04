package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fireynis/the-bell/internal/handler"
)

// --- mock ConfigRepository ---

type mockConfigRepo struct {
	values  map[string]string
	listErr error
	setErr  error
	sets    []string // keys written, in call order
}

func newMockConfigRepo() *mockConfigRepo {
	return &mockConfigRepo{values: make(map[string]string)}
}

func (m *mockConfigRepo) SetTownConfig(_ context.Context, key, value string) error {
	if m.setErr != nil {
		return m.setErr
	}
	m.values[key] = value
	m.sets = append(m.sets, key)
	return nil
}

func (m *mockConfigRepo) GetTownConfig(_ context.Context, key string) (string, error) {
	return m.values[key], nil
}

func (m *mockConfigRepo) ListTownConfig(_ context.Context) (map[string]string, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.values, nil
}

// --- GetConfig ---

func TestConfigHandler_GetConfig(t *testing.T) {
	repo := newMockConfigRepo()
	repo.values["town_name"] = "Bellville"
	repo.values["bootstrap_mode"] = "true"
	h := handler.NewConfigHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	rec := httptest.NewRecorder()

	h.GetConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body map[string]string
	decodeBody(t, rec, &body)
	if body["town_name"] != "Bellville" {
		t.Errorf("town_name = %q, want %q", body["town_name"], "Bellville")
	}
	if _, ok := body["bootstrap_mode"]; ok {
		t.Errorf("response exposed bootstrap_mode: %v", body)
	}
}

func TestConfigHandler_GetConfig_StoreError(t *testing.T) {
	repo := newMockConfigRepo()
	repo.listErr = errors.New("db connection lost")
	h := handler.NewConfigHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	rec := httptest.NewRecorder()

	h.GetConfig(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// --- UpdateConfig ---

func updateConfigRequest(body string) *http.Request {
	return httptest.NewRequest(http.MethodPut, "/api/v1/admin/config", strings.NewReader(body))
}

func TestConfigHandler_UpdateConfig(t *testing.T) {
	repo := newMockConfigRepo()
	h := handler.NewConfigHandler(repo)

	rec := httptest.NewRecorder()
	h.UpdateConfig(rec, updateConfigRequest(`{"town_name":"Bellville","accent_color":"#abcdef"}`))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if repo.values["town_name"] != "Bellville" {
		t.Errorf("town_name = %q, want %q", repo.values["town_name"], "Bellville")
	}
	if repo.values["accent_color"] != "#abcdef" {
		t.Errorf("accent_color = %q, want %q", repo.values["accent_color"], "#abcdef")
	}
}

func TestConfigHandler_UpdateConfig_InvalidJSON(t *testing.T) {
	repo := newMockConfigRepo()
	h := handler.NewConfigHandler(repo)

	rec := httptest.NewRecorder()
	h.UpdateConfig(rec, updateConfigRequest(`{bad}`))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestConfigHandler_UpdateConfig_DisallowedKey(t *testing.T) {
	repo := newMockConfigRepo()
	h := handler.NewConfigHandler(repo)

	rec := httptest.NewRecorder()
	h.UpdateConfig(rec, updateConfigRequest(`{"bootstrap_mode":"false"}`))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// A rejected request must leave the store exactly as it was. The handler used
// to write as it validated, so a valid key iterated before the invalid one was
// already committed when the 400 went out — and map order made it random.
func TestConfigHandler_UpdateConfig_RejectedRequestWritesNothing(t *testing.T) {
	repo := newMockConfigRepo()
	h := handler.NewConfigHandler(repo)

	rec := httptest.NewRecorder()
	h.UpdateConfig(rec, updateConfigRequest(`{"town_name":"Bellville","bootstrap_mode":"false"}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if len(repo.sets) != 0 {
		t.Errorf("store received writes %v, want none", repo.sets)
	}
}

func TestConfigHandler_UpdateConfig_StoreError(t *testing.T) {
	repo := newMockConfigRepo()
	repo.setErr = errors.New("db write failed")
	h := handler.NewConfigHandler(repo)

	rec := httptest.NewRecorder()
	h.UpdateConfig(rec, updateConfigRequest(`{"town_name":"Bellville"}`))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

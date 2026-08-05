package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fireynis/the-bell/internal/handler"
	"github.com/fireynis/the-bell/internal/service"
)

// --- mock ConfigRepository ---

type mockConfigRepo struct {
	values    map[string]string
	listErr   error
	setErr    error
	failAfter int
	sets      []string // keys written, in call order
}

func newMockConfigRepo() *mockConfigRepo {
	return &mockConfigRepo{values: make(map[string]string)}
}

func (m *mockConfigRepo) SetTownConfig(_ context.Context, key, value string) error {
	if m.setErr != nil {
		return m.setErr
	}
	// failAfter simulates a write that succeeds for the first n keys and then
	// fails, which is what a dropped connection partway through the loop looks
	// like. Zero means never fail.
	if m.failAfter > 0 && len(m.sets) >= m.failAfter {
		return errors.New("connection reset partway through")
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

// mockTransactor models a database transaction over mockConfigRepo: it takes a
// snapshot, hands the repo to fn, and restores the snapshot when fn fails —
// which is what a rollback does.
//
// The restore is the point. Without it the rollback test would pass merely
// because the handler called InTx, proving nothing about whether a failed
// update leaves anything behind.
type mockTransactor struct {
	config *mockConfigRepo
	txErr  error // a failure to begin or commit, as opposed to fn failing
}

func (m *mockTransactor) InTx(_ context.Context, fn func(service.UserRepository, service.ConfigRepository) error) error {
	if m.txErr != nil {
		return m.txErr
	}

	snapshot := make(map[string]string, len(m.config.values))
	for k, v := range m.config.values {
		snapshot[k] = v
	}
	writesBefore := len(m.config.sets)

	if err := fn(nil, m.config); err != nil {
		m.config.values = snapshot
		m.config.sets = m.config.sets[:writesBefore]
		return err
	}
	return nil
}

// newConfigHandler builds a handler over repo with a transactor that really
// rolls back.
func newConfigHandler(repo *mockConfigRepo) *handler.ConfigHandler {
	return handler.NewConfigHandler(repo, &mockTransactor{config: repo})
}

// --- GetConfig ---

func TestConfigHandler_GetConfig(t *testing.T) {
	repo := newMockConfigRepo()
	repo.values["town_name"] = "Bellville"
	repo.values["bootstrap_mode"] = "true"
	h := newConfigHandler(repo)

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
	h := newConfigHandler(repo)

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
	h := newConfigHandler(repo)

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
	h := newConfigHandler(repo)

	rec := httptest.NewRecorder()
	h.UpdateConfig(rec, updateConfigRequest(`{bad}`))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestConfigHandler_UpdateConfig_DisallowedKey(t *testing.T) {
	repo := newMockConfigRepo()
	h := newConfigHandler(repo)

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
	h := newConfigHandler(repo)

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
	h := newConfigHandler(repo)

	rec := httptest.NewRecorder()
	h.UpdateConfig(rec, updateConfigRequest(`{"town_name":"Bellville"}`))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// A rejected key already leaves nothing written, because validation completes
// before the first write. A failing *write* should behave the same way: the
// endpoint is documented as all-or-nothing, and a half-applied theme change is
// exactly the state an admin cannot diagnose from the 500 they receive.
//
// A write that fails partway must leave nothing behind. Before the transaction
// the earlier keys stayed applied while the response said 500, and map order
// decided which ones — so the same failure produced a different surviving
// subset run to run.
func TestConfigHandler_UpdateConfig_FailedWriteRollsBack(t *testing.T) {
	repo := newMockConfigRepo()
	repo.failAfter = 1 // first key writes, second fails
	h := newConfigHandler(repo)

	rec := httptest.NewRecorder()
	h.UpdateConfig(rec, updateConfigRequest(`{"town_name":"Bellville","accent_color":"#c62828"}`))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if len(repo.values) != 0 {
		t.Errorf("config store holds %v after a failed write, want nothing persisted", repo.values)
	}
}

// A server wired without a transactor must refuse the write rather than fall
// back to the unprotected loop. Falling back would compile, answer 204, and
// silently reintroduce the partial-write bug — the failure mode this endpoint
// was just fixed for.
func TestConfigHandler_UpdateConfig_WithoutTransactorRefuses(t *testing.T) {
	repo := newMockConfigRepo()
	h := handler.NewConfigHandler(repo, nil)

	rec := httptest.NewRecorder()
	h.UpdateConfig(rec, updateConfigRequest(`{"town_name":"Bellville"}`))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if len(repo.sets) != 0 {
		t.Errorf("store received writes %v without a transaction, want none", repo.sets)
	}
}

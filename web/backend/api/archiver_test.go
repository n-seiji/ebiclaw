package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sipeed/picoclaw/pkg/archiver"
)

type fakeStore struct {
	cfg map[string]any
}

func (s *fakeStore) Get() map[string]any        { return s.cfg }
func (s *fakeStore) Put(c map[string]any) error { s.cfg = c; return nil }

func newFakeStore() ArchiverConfigStore {
	return &fakeStore{cfg: map[string]any{"enabled": false}}
}

type fakeRunner struct {
	runErr error
	status ArchiverStatusSnapshot
}

func (r *fakeRunner) RunOnce(ctx context.Context) error { return r.runErr }
func (r *fakeRunner) Status() ArchiverStatusSnapshot    { return r.status }

func TestArchiver_GetConfig(t *testing.T) {
	h := NewArchiverHandler(newFakeStore(), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/archiver/config", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v, _ := got["enabled"].(bool); v {
		t.Fatalf("expected enabled=false, got %v", got)
	}
}

func TestArchiver_PutConfig(t *testing.T) {
	store := &fakeStore{}
	h := NewArchiverHandler(store, nil)
	body, _ := json.Marshal(map[string]any{
		"enabled":         true,
		"repository_path": "/tmp/x",
		"allowlist":       []string{"slack/C1"},
	})
	req := httptest.NewRequest(http.MethodPut, "/api/archiver/config", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if v, _ := store.cfg["repository_path"].(string); v != "/tmp/x" {
		t.Fatalf("store not updated: %+v", store.cfg)
	}
}

func TestArchiver_PutConfig_BadJSON(t *testing.T) {
	h := NewArchiverHandler(newFakeStore(), nil)
	req := httptest.NewRequest(http.MethodPut, "/api/archiver/config", bytes.NewReader([]byte("not json")))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestArchiver_Status_NoRunner(t *testing.T) {
	h := NewArchiverHandler(newFakeStore(), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/archiver/status", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var got ArchiverStatusSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Running {
		t.Fatalf("expected running=false when no runner")
	}
}

func TestArchiver_Status_WithRunner(t *testing.T) {
	runner := &fakeRunner{status: ArchiverStatusSnapshot{Running: true, ConsecutivePushFailures: 3}}
	h := NewArchiverHandler(newFakeStore(), runner)
	req := httptest.NewRequest(http.MethodGet, "/api/archiver/status", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var got ArchiverStatusSnapshot
	json.Unmarshal(rec.Body.Bytes(), &got)
	if !got.Running || got.ConsecutivePushFailures != 3 {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestArchiver_Run_NoRunner(t *testing.T) {
	h := NewArchiverHandler(newFakeStore(), nil)
	req := httptest.NewRequest(http.MethodPost, "/api/archiver/run", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503; got %d", rec.Code)
	}
}

func TestArchiver_Run_OK(t *testing.T) {
	h := NewArchiverHandler(newFakeStore(), &fakeRunner{})
	req := httptest.NewRequest(http.MethodPost, "/api/archiver/run", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202; got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestArchiver_Run_Busy(t *testing.T) {
	h := NewArchiverHandler(newFakeStore(), &fakeRunner{runErr: archiver.ErrBusy})
	req := httptest.NewRequest(http.MethodPost, "/api/archiver/run", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409; got %d", rec.Code)
	}
}

func TestArchiver_Run_OtherError(t *testing.T) {
	h := NewArchiverHandler(newFakeStore(), &fakeRunner{runErr: errors.New("boom")})
	req := httptest.NewRequest(http.MethodPost, "/api/archiver/run", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500; got %d", rec.Code)
	}
}

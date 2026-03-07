package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/watchword/watchword/internal/repository"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	repo, err := repository.NewSQLiteRepo(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteRepo: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	if err := repo.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewServer(repo, logger, "test", "sqlite")
}

func TestLiveness(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz/live", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "alive" {
		t.Errorf("expected status=alive, got %s", resp["status"])
	}
}

func TestReadiness(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz/ready", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ready" {
		t.Errorf("expected status=ready, got %s", resp["status"])
	}
}

func TestStatus(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["version"] != "test" {
		t.Errorf("expected version=test, got %v", resp["version"])
	}
	if resp["db_driver"] != "sqlite" {
		t.Errorf("expected db_driver=sqlite, got %v", resp["db_driver"])
	}
	if resp["db_status"] != "ok" {
		t.Errorf("expected db_status=ok, got %v", resp["db_status"])
	}
}

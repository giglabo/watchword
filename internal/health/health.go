package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	"github.com/watchword/watchword/internal/repository"
)

type Server struct {
	repo      repository.Repository
	logger    *slog.Logger
	startTime time.Time
	version   string
	dbDriver  string
}

func NewServer(repo repository.Repository, logger *slog.Logger, version string, dbDriver string) *Server {
	return &Server{
		repo:      repo,
		logger:    logger,
		startTime: time.Now(),
		version:   version,
		dbDriver:  dbDriver,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz/live", s.liveness)
	mux.HandleFunc("/healthz/ready", s.readiness)
	mux.HandleFunc("/status", s.status)
	return mux
}

func (s *Server) liveness(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "alive"})
}

func (s *Server) readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	w.Header().Set("Content-Type", "application/json")

	if err := s.repo.Ping(ctx); err != nil {
		s.logger.Warn("readiness check failed", "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]any{
			"status": "not ready",
			"error":  err.Error(),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	dbStatus := "ok"
	if err := s.repo.Ping(ctx); err != nil {
		dbStatus = "error: " + err.Error()
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"version":      s.version,
		"uptime":       time.Since(s.startTime).String(),
		"go_version":   runtime.Version(),
		"goroutines":   runtime.NumGoroutine(),
		"db_driver":    s.dbDriver,
		"db_status":    dbStatus,
		"alloc_mb":     float64(mem.Alloc) / 1024 / 1024,
		"sys_mb":       float64(mem.Sys) / 1024 / 1024,
	})
}

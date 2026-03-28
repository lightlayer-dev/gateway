// Package admin provides the admin HTTP server for the LightLayer Gateway.
// It runs on a separate port (default 9090) and serves REST API endpoints
// for health, metrics, config management, and a WebSocket for live logs.
package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lightlayer-dev/gateway/internal/config"
	"github.com/lightlayer-dev/gateway/internal/plugins"
	"github.com/lightlayer-dev/gateway/internal/store"
)

// Server is the admin HTTP server.
type Server struct {
	httpSrv   *http.Server
	cfg       *config.Config
	cfgMu     sync.RWMutex
	pipeline  *plugins.Pipeline
	store     store.Store
	startTime time.Time
	version   string
	logHub    *LogHub

	// ReloadFunc is called when config reload is triggered via API.
	ReloadFunc func(path string) error

	// ConfigPath is the path to the active config file.
	ConfigPath string
}

// NewServer creates a new admin server.
func NewServer(cfg *config.Config, pipeline *plugins.Pipeline, st store.Store, version string) *Server {
	s := &Server{
		cfg:       cfg,
		pipeline:  pipeline,
		store:     st,
		startTime: time.Now(),
		version:   version,
		logHub:    NewLogHub(),
	}
	return s
}

// Start begins listening on the configured admin port.
func (s *Server) Start() error {
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	addr := fmt.Sprintf(":%d", s.cfg.Admin.Port)
	s.httpSrv = &http.Server{
		Addr:              addr,
		Handler:           s.authMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	slog.Info("admin listening", "addr", addr)
	go func() {
		if err := s.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("admin server error", "error", err)
		}
	}()
	return nil
}

// Shutdown gracefully stops the admin server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logHub.Close()
	if s.httpSrv != nil {
		return s.httpSrv.Shutdown(ctx)
	}
	return nil
}

// SetConfig atomically updates the config reference.
func (s *Server) SetConfig(cfg *config.Config) {
	s.cfgMu.Lock()
	s.cfg = cfg
	s.cfgMu.Unlock()
}

// GetConfig returns the current config.
func (s *Server) GetConfig() *config.Config {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg
}

// SetPipeline updates the pipeline reference after a hot reload.
func (s *Server) SetPipeline(pipeline *plugins.Pipeline) {
	s.pipeline = pipeline
}

// SetStore updates the store reference.
func (s *Server) SetStore(st store.Store) {
	s.store = st
}

// LogHub returns the live log hub for broadcasting events.
func (s *Server) LogHub() *LogHub {
	return s.logHub
}

// authMiddleware checks the admin auth token if configured.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := s.GetConfig()
		if cfg.Admin.AuthToken == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Allow health check without auth.
		if r.URL.Path == "/api/health" || r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}

		token := r.Header.Get("Authorization")
		token = strings.TrimPrefix(token, "Bearer ")
		if token == "" {
			token = r.URL.Query().Get("token")
		}

		if token != cfg.Admin.AuthToken {
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "unauthorized",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

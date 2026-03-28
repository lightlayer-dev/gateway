package admin

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lightlayer-dev/gateway/internal/config"
	"github.com/lightlayer-dev/gateway/internal/store"
	"gopkg.in/yaml.v3"
)

// registerRoutes sets up all admin API endpoints.
func (s *Server) registerRoutes(mux *http.ServeMux) {
	// Health & status
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /api/status", s.handleStatus)

	// Metrics & analytics
	mux.HandleFunc("GET /api/metrics", s.handleMetrics)
	mux.HandleFunc("GET /api/analytics", s.handleAnalytics)
	mux.HandleFunc("GET /api/agents", s.handleAgents)

	// Config management
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("PUT /api/config", s.handleUpdateConfig)
	mux.HandleFunc("PUT /api/config/plugins", s.handleUpdatePluginConfig)
	mux.HandleFunc("GET /api/config/export", s.handleExportConfig)
	mux.HandleFunc("POST /api/config/import", s.handleImportConfig)

	// API keys
	mux.HandleFunc("POST /api/keys", s.handleCreateKey)
	mux.HandleFunc("GET /api/keys", s.handleListKeys)
	mux.HandleFunc("DELETE /api/keys/{id}", s.handleDeleteKey)

	// A2A tasks
	mux.HandleFunc("GET /api/a2a/tasks", s.handleA2ATasks)

	// WebSocket
	mux.HandleFunc("GET /api/ws/logs", s.handleWSLogs)
}

// ── Health & Status ──────────────────────────────────────────────────────

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"version": s.version,
		"uptime":  time.Since(s.startTime).String(),
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	cfg := s.GetConfig()

	pluginStates := make([]map[string]interface{}, 0)
	if s.pipeline != nil {
		for _, p := range s.pipeline.Plugins() {
			pluginStates = append(pluginStates, map[string]interface{}{
				"name":   p.Name(),
				"active": true,
			})
		}
	}

	status := map[string]interface{}{
		"status":        "running",
		"version":       s.version,
		"uptime":        time.Since(s.startTime).String(),
		"uptime_seconds": time.Since(s.startTime).Seconds(),
		"origin_url":    cfg.Gateway.Origin.URL,
		"listen_port":   cfg.Gateway.Listen.Port,
		"admin_port":    cfg.Admin.Port,
		"plugins":       pluginStates,
	}

	// Add request count from metrics if store is available.
	if s.store != nil {
		tr := store.TimeRange{
			From: s.startTime,
			To:   time.Now(),
		}
		if m, err := s.store.GetMetrics(tr); err == nil {
			status["total_requests"] = m.TotalRequests
			status["unique_agents"] = m.UniqueAgents
		}
	}

	writeJSON(w, http.StatusOK, status)
}

// ── Metrics & Analytics ──────────────────────────────────────────────────

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "analytics store not configured",
		})
		return
	}

	tr := parseTimeRange(r, s.startTime)
	metrics, err := s.store.GetMetrics(tr)
	if err != nil {
		slog.Error("admin: get metrics failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to retrieve metrics",
		})
		return
	}

	writeJSON(w, http.StatusOK, metrics)
}

func (s *Server) handleAnalytics(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "analytics store not configured",
		})
		return
	}

	filter := store.EventFilter{
		Agent:  r.URL.Query().Get("agent"),
		Method: r.URL.Query().Get("method"),
		Path:   r.URL.Query().Get("path"),
		Limit:  100,
	}

	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			filter.Limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			filter.Offset = n
		}
	}
	if v := r.URL.Query().Get("min_status"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			filter.MinStatus = n
		}
	}
	if v := r.URL.Query().Get("max_status"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			filter.MaxStatus = n
		}
	}
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.From = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.To = t
		}
	}

	events, err := s.store.QueryEvents(filter)
	if err != nil {
		slog.Error("admin: query events failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to query analytics",
		})
		return
	}

	// Also get aggregated metrics for the same time range.
	tr := store.TimeRange{From: filter.From, To: filter.To}
	if tr.From.IsZero() {
		tr.From = s.startTime
	}
	if tr.To.IsZero() {
		tr.To = time.Now()
	}
	metrics, _ := s.store.GetMetrics(tr)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"events":  events,
		"metrics": metrics,
	})
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "analytics store not configured",
		})
		return
	}

	// Query distinct agents from events table.
	tr := store.TimeRange{
		From: s.startTime,
		To:   time.Now(),
	}
	metrics, err := s.store.GetMetrics(tr)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to query agents",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"agents": metrics.TopAgents,
	})
}

// ── Config Management ────────────────────────────────────────────────────

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.GetConfig()

	// Sanitize sensitive fields.
	sanitized := *cfg
	sanitized.Admin.AuthToken = ""
	if sanitized.Plugins.OAuth2.ClientSecret != "" {
		sanitized.Plugins.OAuth2.ClientSecret = "***"
	}
	if sanitized.Plugins.Analytics.APIKey != "" {
		sanitized.Plugins.Analytics.APIKey = "***"
	}

	writeJSON(w, http.StatusOK, sanitized)
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	if s.ReloadFunc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "hot reload not configured",
		})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read body"})
		return
	}

	// Parse and validate the new config.
	newCfg, err := config.Parse(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("invalid config: %v", err),
		})
		return
	}
	config.ApplyDefaults(newCfg)
	if err := config.Validate(newCfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("config validation failed: %v", err),
		})
		return
	}

	// Write to config file if we have a path.
	if s.ConfigPath != "" {
		yamlData, err := yaml.Marshal(newCfg)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "failed to marshal config",
			})
			return
		}
		if err := writeConfigFile(s.ConfigPath, yamlData); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": fmt.Sprintf("failed to write config: %v", err),
			})
			return
		}
	}

	// Trigger reload.
	if err := s.ReloadFunc(s.ConfigPath); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("reload failed: %v", err),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "reloaded"})
}

func (s *Server) handleUpdatePluginConfig(w http.ResponseWriter, r *http.Request) {
	if s.ReloadFunc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "hot reload not configured",
		})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read body"})
		return
	}

	// Parse plugin update as a partial config.
	var pluginUpdate config.PluginsConfig
	if err := json.Unmarshal(body, &pluginUpdate); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("invalid plugin config: %v", err),
		})
		return
	}

	// Merge into current config.
	cfg := s.GetConfig()
	merged := *cfg
	merged.Plugins = pluginUpdate

	config.ApplyDefaults(&merged)
	if err := config.Validate(&merged); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("validation failed: %v", err),
		})
		return
	}

	// Write and reload.
	if s.ConfigPath != "" {
		yamlData, err := yaml.Marshal(&merged)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "marshal failed"})
			return
		}
		if err := writeConfigFile(s.ConfigPath, yamlData); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": fmt.Sprintf("write failed: %v", err),
			})
			return
		}
	}

	if err := s.ReloadFunc(s.ConfigPath); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("reload failed: %v", err),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "reloaded"})
}

func (s *Server) handleExportConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.GetConfig()

	data, err := yaml.Marshal(cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to marshal config",
		})
		return
	}

	w.Header().Set("Content-Type", "application/x-yaml")
	w.Header().Set("Content-Disposition", "attachment; filename=gateway.yaml")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

func (s *Server) handleImportConfig(w http.ResponseWriter, r *http.Request) {
	if s.ReloadFunc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "hot reload not configured",
		})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read body"})
		return
	}

	// Validate the uploaded YAML.
	newCfg, err := config.Parse(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("invalid YAML: %v", err),
		})
		return
	}
	config.ApplyDefaults(newCfg)
	if err := config.Validate(newCfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("validation failed: %v", err),
		})
		return
	}

	// Write and reload.
	if s.ConfigPath != "" {
		if err := writeConfigFile(s.ConfigPath, body); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": fmt.Sprintf("write failed: %v", err),
			})
			return
		}
	}

	if err := s.ReloadFunc(s.ConfigPath); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("reload failed: %v", err),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "imported and reloaded"})
}

// ── API Keys ─────────────────────────────────────────────────────────────

func (s *Server) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	// API keys are managed by the apikeys plugin. The admin API provides
	// a thin proxy for creating keys stored in the config.
	var req struct {
		ID        string                 `json:"id"`
		Scopes    []string               `json:"scopes"`
		ExpiresAt string                 `json:"expires_at,omitempty"`
		CompanyID string                 `json:"company_id,omitempty"`
		UserID    string                 `json:"user_id,omitempty"`
		Metadata  map[string]interface{} `json:"metadata,omitempty"`
	}

	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.ID == "" || len(req.Scopes) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "id and scopes are required",
		})
		return
	}

	// Store in config.
	cfg := s.GetConfig()
	newKey := config.APIKeyConfig{
		ID:        req.ID,
		Scopes:    req.Scopes,
		ExpiresAt: req.ExpiresAt,
		CompanyID: req.CompanyID,
		UserID:    req.UserID,
		Metadata:  req.Metadata,
	}
	cfg.Plugins.APIKeys.Keys = append(cfg.Plugins.APIKeys.Keys, newKey)
	s.SetConfig(cfg)

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"key": newKey,
	})
}

func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	cfg := s.GetConfig()

	// Return keys with secrets masked.
	keys := make([]map[string]interface{}, len(cfg.Plugins.APIKeys.Keys))
	for i, k := range cfg.Plugins.APIKeys.Keys {
		keys[i] = map[string]interface{}{
			"id":         k.ID,
			"scopes":     k.Scopes,
			"expires_at": k.ExpiresAt,
			"company_id": k.CompanyID,
			"user_id":    k.UserID,
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"keys": keys,
	})
}

func (s *Server) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key id required"})
		return
	}

	cfg := s.GetConfig()
	found := false
	filtered := make([]config.APIKeyConfig, 0, len(cfg.Plugins.APIKeys.Keys))
	for _, k := range cfg.Plugins.APIKeys.Keys {
		if k.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, k)
	}

	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "key not found"})
		return
	}

	cfg.Plugins.APIKeys.Keys = filtered
	s.SetConfig(cfg)

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ── A2A Tasks ────────────────────────────────────────────────────────────

func (s *Server) handleA2ATasks(w http.ResponseWriter, r *http.Request) {
	// Proxy to the A2A plugin's task store if available.
	// For now, return the tasks from the store if analytics data includes them.
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tasks": []interface{}{},
		"note":  "A2A task listing available via the /a2a JSON-RPC endpoint (tasks/list)",
	})
}

// ── Helpers ──────────────────────────────────────────────────────────────

func parseTimeRange(r *http.Request, defaultFrom time.Time) store.TimeRange {
	tr := store.TimeRange{
		From: defaultFrom,
		To:   time.Now(),
	}

	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			tr.From = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			tr.To = t
		}
	}

	// Support shorthand like "1h", "24h", "7d".
	if v := r.URL.Query().Get("period"); v != "" {
		if d, err := parsePeriod(v); err == nil {
			tr.From = time.Now().Add(-d)
		}
	}

	return tr
}

func parsePeriod(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

func writeConfigFile(path string, data []byte) error {
	return writeFileAtomic(path, data)
}

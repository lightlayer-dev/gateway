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

	// Discovery preview
	mux.HandleFunc("GET /api/discovery/preview", s.handleDiscoveryPreview)

	// Agent activity (from agents table)
	mux.HandleFunc("GET /api/agents/activity", s.handleAgentActivity)

	// Payment history
	mux.HandleFunc("GET /api/payments/history", s.handlePaymentHistory)

	// Demo mode
	mux.HandleFunc("GET /api/demo/status", s.handleDemoStatus)

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

// ── Discovery Preview ─────────────────────────────────────────────────────

func (s *Server) handleDiscoveryPreview(w http.ResponseWriter, r *http.Request) {
	cfg := s.GetConfig()
	d := cfg.Plugins.Discovery

	// Generate /agents.txt content.
	agentsTxt := fmt.Sprintf("# agents.txt for %s\n# Generated by LightLayer Gateway\n\n", d.Name)
	agentsTxt += "User-agent: *\nAllow: /\n"
	for _, cap := range d.Capabilities {
		for _, p := range cap.Paths {
			agentsTxt += fmt.Sprintf("Allow: %s\n", p)
		}
	}

	// Generate /llms.txt content.
	llmsTxt := fmt.Sprintf("# %s\n\n> %s\n\nVersion: %s\n", d.Name, d.Description, d.Version)
	for _, cap := range d.Capabilities {
		llmsTxt += fmt.Sprintf("\n## %s\n%s\nMethods: %s\n", cap.Name, cap.Description, strings.Join(cap.Methods, ", "))
		for _, p := range cap.Paths {
			llmsTxt += fmt.Sprintf("- %s\n", p)
		}
	}

	// Generate /.well-known/agent.json (A2A Agent Card).
	agentCard := map[string]interface{}{
		"name":        d.Name,
		"description": d.Description,
		"version":     d.Version,
		"url":         fmt.Sprintf("http://localhost:%d", cfg.Gateway.Listen.Port),
		"skills":      d.Capabilities,
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"agent_card": agentCard,
		"agents_txt": agentsTxt,
		"llms_txt":   llmsTxt,
	})
}

// ── Agent Activity ────────────────────────────────────────────────────────

func (s *Server) handleAgentActivity(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "analytics store not configured",
		})
		return
	}

	agents, err := s.store.GetAgents()
	if err != nil {
		slog.Error("admin: agent activity query failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to query agent activity",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"agents": agents,
	})
}

// ── Payment History ───────────────────────────────────────────────────────

func (s *Server) handlePaymentHistory(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "analytics store not configured",
		})
		return
	}

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	events, err := s.store.GetPaymentEvents(limit)
	if err != nil {
		slog.Error("admin: payment history query failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to query payment history",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"payments": events,
	})
}

// ── Demo Status ──────────────────────────────────────────────────────────

func (s *Server) handleDemoStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"demo_mode":    s.DemoMode,
		"demo_api_url": s.DemoAPIURL,
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

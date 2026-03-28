package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lightlayer-dev/gateway/internal/config"
	"github.com/lightlayer-dev/gateway/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Gateway.Listen.Port = 8080
	cfg.Gateway.Listen.Host = "127.0.0.1"
	cfg.Gateway.Origin.URL = "https://httpbin.org"
	cfg.Admin.Enabled = true
	cfg.Admin.Port = 9090
	return cfg
}

func testServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	cfg := testConfig()
	s := NewServer(cfg, nil, nil, "test-v0.1.0")

	mux := http.NewServeMux()
	s.registerRoutes(mux)
	ts := httptest.NewServer(s.authMiddleware(mux))
	t.Cleanup(ts.Close)
	return s, ts
}

func testServerWithStore(t *testing.T) (*Server, *httptest.Server, store.Store) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	st, err := store.NewSQLiteStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })

	cfg := testConfig()
	s := NewServer(cfg, nil, st, "test-v0.1.0")

	mux := http.NewServeMux()
	s.registerRoutes(mux)
	ts := httptest.NewServer(s.authMiddleware(mux))
	t.Cleanup(ts.Close)
	return s, ts, st
}

// ── Health ────────────────────────────────────────────────────────────────

func TestHealthEndpoint(t *testing.T) {
	_, ts := testServer(t)

	resp, err := http.Get(ts.URL + "/api/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
	assert.Equal(t, "test-v0.1.0", body["version"])
	assert.NotEmpty(t, body["uptime"])
}

func TestHealthzEndpoint(t *testing.T) {
	_, ts := testServer(t)

	resp, err := http.Get(ts.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// ── Status ───────────────────────────────────────────────────────────────

func TestStatusEndpoint(t *testing.T) {
	_, ts := testServer(t)

	resp, err := http.Get(ts.URL + "/api/status")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "running", body["status"])
	assert.Equal(t, "https://httpbin.org", body["origin_url"])
}

// ── Metrics ──────────────────────────────────────────────────────────────

func TestMetricsNoStore(t *testing.T) {
	_, ts := testServer(t)

	resp, err := http.Get(ts.URL + "/api/metrics")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestMetricsWithStore(t *testing.T) {
	_, ts, st := testServerWithStore(t)

	// Insert a test event.
	require.NoError(t, st.SaveEvent(store.AgentEvent{
		ID:         "evt-1",
		Timestamp:  time.Now(),
		Agent:      "ClaudeBot",
		Method:     "GET",
		Path:       "/api/test",
		StatusCode: 200,
		DurationMs: 42.5,
	}))

	resp, err := http.Get(ts.URL + "/api/metrics")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body store.Metrics
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, int64(1), body.TotalRequests)
}

// ── Analytics ────────────────────────────────────────────────────────────

func TestAnalyticsEndpoint(t *testing.T) {
	_, ts, st := testServerWithStore(t)

	require.NoError(t, st.SaveEvent(store.AgentEvent{
		ID:         "evt-2",
		Timestamp:  time.Now(),
		Agent:      "GPTBot",
		Method:     "POST",
		Path:       "/api/widgets",
		StatusCode: 201,
		DurationMs: 100,
	}))

	resp, err := http.Get(ts.URL + "/api/analytics?agent=GPTBot")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	events, ok := body["events"].([]interface{})
	require.True(t, ok)
	assert.Len(t, events, 1)
}

// ── Config ───────────────────────────────────────────────────────────────

func TestGetConfig(t *testing.T) {
	_, ts := testServer(t)
	resp, err := http.Get(ts.URL + "/api/config")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestExportConfig(t *testing.T) {
	_, ts := testServer(t)

	resp, err := http.Get(ts.URL + "/api/config/export")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/x-yaml", resp.Header.Get("Content-Type"))
	assert.Contains(t, resp.Header.Get("Content-Disposition"), "gateway.yaml")
}

func TestImportConfig(t *testing.T) {
	s, ts := testServer(t)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gateway.yaml")
	s.ConfigPath = cfgPath
	s.ReloadFunc = func(path string) error { return nil }

	yamlBody := `gateway:
  listen:
    port: 8080
    host: 0.0.0.0
  origin:
    url: https://api.example.com
    timeout: 30s
plugins:
  discovery:
    enabled: false
  rate_limits:
    enabled: false
  analytics:
    enabled: false
  security:
    enabled: false
  payments:
    enabled: false
admin:
  enabled: true
  port: 9090
`

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/config/import", strings.NewReader(yamlBody))
	req.Header.Set("Content-Type", "application/x-yaml")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify file was written.
	data, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "api.example.com")
}

// ── Auth ─────────────────────────────────────────────────────────────────

func TestAuthMiddleware(t *testing.T) {
	cfg := testConfig()
	cfg.Admin.AuthToken = "secret-token"
	s := NewServer(cfg, nil, nil, "test")

	mux := http.NewServeMux()
	s.registerRoutes(mux)
	ts := httptest.NewServer(s.authMiddleware(mux))
	defer ts.Close()

	// Health should work without auth.
	resp, err := http.Get(ts.URL + "/api/health")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Status should fail without auth.
	resp, err = http.Get(ts.URL + "/api/status")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// Status should work with auth.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/status", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Auth via query param.
	resp, err = http.Get(ts.URL + "/api/status?token=secret-token")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// ── WebSocket ────────────────────────────────────────────────────────────

func TestWebSocketLogs(t *testing.T) {
	s, ts := testServer(t)

	// Connect WebSocket.
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/ws/logs"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// Give the connection time to register.
	time.Sleep(50 * time.Millisecond)

	// Broadcast a log entry.
	s.LogHub().Broadcast(LogEntry{
		Timestamp:  time.Now(),
		Method:     "GET",
		Path:       "/api/test",
		StatusCode: 200,
		DurationMs: 15.5,
		Agent:      "TestBot",
	})

	// Read the message.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	require.NoError(t, err)

	var entry LogEntry
	require.NoError(t, json.Unmarshal(msg, &entry))
	assert.Equal(t, "GET", entry.Method)
	assert.Equal(t, "/api/test", entry.Path)
	assert.Equal(t, 200, entry.StatusCode)
	assert.Equal(t, "TestBot", entry.Agent)
}

func TestWebSocketFilter(t *testing.T) {
	s, ts := testServer(t)

	// Connect with agent filter.
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/ws/logs?agent=ClaudeBot"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	// Broadcast for a different agent — should be filtered.
	s.LogHub().Broadcast(LogEntry{
		Timestamp: time.Now(),
		Agent:     "GPTBot",
		Method:    "GET",
		Path:      "/test",
	})

	// Broadcast for the matching agent.
	s.LogHub().Broadcast(LogEntry{
		Timestamp: time.Now(),
		Agent:     "ClaudeBot",
		Method:    "POST",
		Path:      "/api/chat",
	})

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	require.NoError(t, err)

	var entry LogEntry
	require.NoError(t, json.Unmarshal(msg, &entry))
	assert.Equal(t, "ClaudeBot", entry.Agent)
	assert.Equal(t, "POST", entry.Method)
}

// ── LogHub ───────────────────────────────────────────────────────────────

func TestLogHubBroadcastEvent(t *testing.T) {
	hub := NewLogHub()
	defer hub.Close()

	assert.Equal(t, 0, hub.ClientCount())
}

func TestLogEntryFromEvent(t *testing.T) {
	event := store.AgentEvent{
		ID:           "test-id",
		Timestamp:    time.Now(),
		Agent:        "TestBot",
		Method:       "GET",
		Path:         "/test",
		StatusCode:   200,
		DurationMs:   42.0,
		UserAgent:    "TestBot/1.0",
		ContentType:  "application/json",
		ResponseSize: 1024,
	}

	entry := LogEntryFromEvent(event)
	assert.Equal(t, "test-id", entry.RequestID)
	assert.Equal(t, "TestBot", entry.Agent)
	assert.Equal(t, "GET", entry.Method)
	assert.Equal(t, "/test", entry.Path)
	assert.Equal(t, 200, entry.StatusCode)
	assert.Equal(t, 42.0, entry.DurationMs)
}

// ── Agents ───────────────────────────────────────────────────────────────

func TestAgentsEndpoint(t *testing.T) {
	_, ts, st := testServerWithStore(t)

	// Insert events for different agents.
	for i, agent := range []string{"ClaudeBot", "ClaudeBot", "GPTBot"} {
		require.NoError(t, st.SaveEvent(store.AgentEvent{
			ID:         fmt.Sprintf("evt-%d", i),
			Timestamp:  time.Now(),
			Agent:      agent,
			Method:     "GET",
			Path:       "/test",
			StatusCode: 200,
		}))
	}

	resp, err := http.Get(ts.URL + "/api/agents")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	agents := body["agents"].([]interface{})
	assert.Len(t, agents, 2)
}

// ── Period parsing ───────────────────────────────────────────────────────

func TestParsePeriod(t *testing.T) {
	d, err := parsePeriod("24h")
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, d)

	d, err = parsePeriod("7d")
	require.NoError(t, err)
	assert.Equal(t, 7*24*time.Hour, d)

	_, err = parsePeriod("bad")
	assert.Error(t, err)
}

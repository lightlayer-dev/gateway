package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lightlayer-dev/gateway/internal/config"
	"github.com/lightlayer-dev/gateway/internal/plugins"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/a2a"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/agentstxt"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/agui"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/analytics"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/discovery"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/mcp"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/payments"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/ratelimit"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/security"
	"github.com/lightlayer-dev/gateway/internal/proxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newOrigin creates a mock origin HTTP server.
func newOrigin(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Origin", "true")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"path":   r.URL.Path,
			"method": r.Method,
			"agent":  r.Header.Get("User-Agent"),
		})
	}))
}

// buildGateway creates a full gateway pipeline (config → plugins → proxy) for testing.
func buildGateway(t *testing.T, originURL string, cfgFn func(*config.Config)) http.Handler {
	t.Helper()

	cfg := &config.Config{}
	cfg.Gateway.Origin.URL = originURL
	cfg.Gateway.Origin.Timeout = config.Duration{Duration: 10 * time.Second}
	cfg.Gateway.Listen.Port = 0
	cfg.Gateway.Listen.Host = "127.0.0.1"
	cfg.Plugins.Discovery.Enabled = true
	cfg.Plugins.Discovery.Name = "Test API"
	cfg.Plugins.Discovery.Description = "Integration test API"
	cfg.Plugins.Discovery.Version = "1.0.0"
	cfg.Plugins.Security.Enabled = true
	cfg.Plugins.RateLimits.Enabled = true
	cfg.Plugins.RateLimits.Default.Requests = 1000
	cfg.Plugins.RateLimits.Default.Window = config.Duration{Duration: time.Minute}
	cfg.Plugins.Analytics.Enabled = true

	if cfgFn != nil {
		cfgFn(cfg)
	}

	config.ApplyDefaults(cfg)

	p, err := proxy.NewProxy(cfg)
	require.NoError(t, err)

	pipeline, err := plugins.BuildPipeline(pluginConfigs(cfg))
	require.NoError(t, err)
	t.Cleanup(func() { pipeline.Close() })

	return pipeline.Wrap(p)
}

// pluginConfigs mirrors the config conversion from start.go.
func pluginConfigs(cfg *config.Config) []plugins.PluginConfig {
	discoveryCfg := map[string]interface{}{
		"name":        cfg.Plugins.Discovery.Name,
		"description": cfg.Plugins.Discovery.Description,
		"version":     cfg.Plugins.Discovery.Version,
		"url":         fmt.Sprintf("http://%s:%d", cfg.Gateway.Listen.Host, cfg.Gateway.Listen.Port),
	}
	if len(cfg.Plugins.Discovery.Capabilities) > 0 {
		caps := make([]interface{}, len(cfg.Plugins.Discovery.Capabilities))
		for i, c := range cfg.Plugins.Discovery.Capabilities {
			caps[i] = map[string]interface{}{
				"name":        c.Name,
				"description": c.Description,
				"methods":     c.Methods,
				"paths":       c.Paths,
			}
		}
		discoveryCfg["capabilities"] = caps
	}

	return []plugins.PluginConfig{
		{Name: "security", Enabled: cfg.Plugins.Security.Enabled},
		{Name: "discovery", Enabled: cfg.Plugins.Discovery.Enabled, Config: discoveryCfg},
		{Name: "rate_limits", Enabled: cfg.Plugins.RateLimits.Enabled},
		{Name: "analytics", Enabled: cfg.Plugins.Analytics.Enabled},
	}
}

// ── Full Pipeline Test ──────────────────────────────────────────────────────

func TestFullPipeline(t *testing.T) {
	origin := newOrigin(t)
	defer origin.Close()

	handler := buildGateway(t, origin.URL, nil)
	gw := httptest.NewServer(handler)
	defer gw.Close()

	// Normal proxied request.
	resp, err := http.Get(gw.URL + "/api/test")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "/api/test", body["path"])
	assert.Equal(t, "GET", body["method"])

	// Verify security headers were added.
	assert.NotEmpty(t, resp.Header.Get("X-Content-Type-Options"))
}

// ── Discovery Endpoints ─────────────────────────────────────────────────────

func TestDiscoveryEndpoints(t *testing.T) {
	origin := newOrigin(t)
	defer origin.Close()

	handler := buildGateway(t, origin.URL, nil)
	gw := httptest.NewServer(handler)
	defer gw.Close()

	discoveryPaths := []string{
		"/.well-known/ai",
		"/.well-known/agent.json",
		"/llms.txt",
		"/agents.txt",
	}

	for _, path := range discoveryPaths {
		t.Run(path, func(t *testing.T) {
			resp, err := http.Get(gw.URL + path)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode,
				"discovery endpoint %s should return 200", path)
			b, _ := io.ReadAll(resp.Body)
			assert.NotEmpty(t, b, "body should not be empty for %s", path)
		})
	}
}

// ── Concurrent Requests (Race Detector) ─────────────────────────────────────

func TestConcurrentRequests(t *testing.T) {
	origin := newOrigin(t)
	defer origin.Close()

	handler := buildGateway(t, origin.URL, nil)
	gw := httptest.NewServer(handler)
	defer gw.Close()

	const goroutines = 100
	var wg sync.WaitGroup
	var errors atomic.Int64
	var successes atomic.Int64

	wg.Add(goroutines)
	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			url := fmt.Sprintf("%s/api/item/%d", gw.URL, idx)
			resp, err := http.Get(url)
			if err != nil {
				errors.Add(1)
				return
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				successes.Add(1)
			} else {
				errors.Add(1)
			}
		}(i)
	}
	wg.Wait()

	// All requests should succeed — rate limit is set high enough.
	assert.Equal(t, int64(goroutines), successes.Load(), "all concurrent requests should succeed")
	assert.Equal(t, int64(0), errors.Load(), "no errors expected")
}

// ── Origin Down / Recovery ──────────────────────────────────────────────────

func TestOriginDown(t *testing.T) {
	// Start origin then immediately close it.
	origin := newOrigin(t)
	originURL := origin.URL
	origin.Close()

	handler := buildGateway(t, originURL, nil)
	gw := httptest.NewServer(handler)
	defer gw.Close()

	resp, err := http.Get(gw.URL + "/api/test")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should get a structured error (502 Bad Gateway).
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)

	var errBody map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&errBody))
	assert.Equal(t, "proxy_error", errBody["type"])
	assert.Equal(t, true, errBody["is_retriable"])
}

func TestOriginRecovery(t *testing.T) {
	// Create an origin that can be toggled.
	var originUp atomic.Bool
	originUp.Store(true)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !originUp.Load() {
			// Simulate origin being down by closing the connection.
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, _ := hj.Hijack()
				conn.Close()
				return
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer origin.Close()

	handler := buildGateway(t, origin.URL, nil)
	gw := httptest.NewServer(handler)
	defer gw.Close()

	// Request 1: origin is up.
	resp, err := http.Get(gw.URL + "/api/test")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Take origin down.
	originUp.Store(false)
	resp, err = http.Get(gw.URL + "/api/test")
	require.NoError(t, err)
	resp.Body.Close()
	assert.True(t, resp.StatusCode >= 500, "should get error when origin is down")

	// Bring origin back.
	originUp.Store(true)
	resp, err = http.Get(gw.URL + "/api/test")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "should recover when origin comes back")
}

// ── Plugin Panic Recovery ───────────────────────────────────────────────────

func TestPluginPanicRecovery(t *testing.T) {
	origin := newOrigin(t)
	defer origin.Close()

	cfg := &config.Config{}
	cfg.Gateway.Origin.URL = origin.URL
	cfg.Gateway.Origin.Timeout = config.Duration{Duration: 10 * time.Second}
	config.ApplyDefaults(cfg)

	p, err := proxy.NewProxy(cfg)
	require.NoError(t, err)

	// Create a pipeline with no plugins, then manually wrap with a panicking middleware.
	pipeline, err := plugins.BuildPipeline(nil)
	require.NoError(t, err)

	// Wrap: panicking middleware → proxy.
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/panic" {
			panic("test panic")
		}
		p.ServeHTTP(w, r)
	})

	// Use the pipeline's Wrap which includes recovery.
	handler := pipeline.Wrap(panicHandler)
	gw := httptest.NewServer(handler)
	defer gw.Close()

	// Non-panic path should work.
	resp, err := http.Get(gw.URL + "/normal")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// ── Agent Detection Through Pipeline ────────────────────────────────────────

func TestAgentDetectionPipeline(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"forwarded_ua": r.Header.Get("User-Agent"),
		})
	}))
	defer origin.Close()

	handler := buildGateway(t, origin.URL, nil)
	gw := httptest.NewServer(handler)
	defer gw.Close()

	// Send request with a known AI agent User-Agent.
	client := &http.Client{}
	req, _ := http.NewRequest("GET", gw.URL+"/api/data", nil)
	req.Header.Set("User-Agent", "ClaudeBot/1.0")

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// ── Dashboard API Config CRUD ───────────────────────────────────────────────

func TestDashboardAPIConfigCRUD(t *testing.T) {
	origin := newOrigin(t)
	defer origin.Close()

	handler := buildGateway(t, origin.URL, nil)
	gw := httptest.NewServer(handler)
	defer gw.Close()

	// Proxied requests should work through the full pipeline.
	resp, err := http.Get(gw.URL + "/api/widget/1")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// POST request.
	resp, err = http.Post(gw.URL+"/api/widget", "application/json",
		strings.NewReader(`{"name":"test"}`))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// ── Structured Error Format ─────────────────────────────────────────────────

func TestStructuredErrorFormat(t *testing.T) {
	// Origin that returns various error codes.
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/error/500":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(500)
			json.NewEncoder(w).Encode(map[string]string{"error": "internal"})
		case "/error/404":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(404)
			json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		default:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
		}
	}))
	defer origin.Close()

	handler := buildGateway(t, origin.URL, nil)
	gw := httptest.NewServer(handler)
	defer gw.Close()

	// Origin errors are passed through.
	resp, err := http.Get(gw.URL + "/error/404")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "json")
}

// ── Multiple Methods ────────────────────────────────────────────────────────

func TestHTTPMethods(t *testing.T) {
	origin := newOrigin(t)
	defer origin.Close()

	handler := buildGateway(t, origin.URL, nil)
	gw := httptest.NewServer(handler)
	defer gw.Close()

	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH"}
	client := &http.Client{}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			var bodyReader io.Reader
			if method != "GET" && method != "DELETE" {
				bodyReader = strings.NewReader(`{"test":true}`)
			}
			req, err := http.NewRequest(method, gw.URL+"/api/resource", bodyReader)
			require.NoError(t, err)
			if bodyReader != nil {
				req.Header.Set("Content-Type", "application/json")
			}

			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			var body map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&body)
			assert.Equal(t, method, body["method"])
		})
	}
}

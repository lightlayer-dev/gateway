package discovery

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// baseConfig returns a minimal config map for testing.
func baseConfig() map[string]interface{} {
	return map[string]interface{}{
		"name":        "Test API",
		"description": "A test API",
		"url":         "https://api.example.com",
		"version":     "1.0.0",
		"skills": []interface{}{
			map[string]interface{}{
				"id":          "widgets",
				"name":        "Widget Management",
				"description": "CRUD operations for widgets",
				"tags":        []interface{}{"widgets", "crud"},
				"examples":    []interface{}{"List all widgets", "Create a widget"},
			},
		},
	}
}

func setupPlugin(t *testing.T, cfg map[string]interface{}) *Plugin {
	t.Helper()
	p := New()
	require.NoError(t, p.Init(cfg))
	return p
}

func serveRequest(p *Plugin, path string) *httptest.ResponseRecorder {
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot) // distinguishable from discovery responses
		w.Write([]byte("backend"))
	})
	handler := p.Middleware()(backend)
	req := httptest.NewRequest("GET", path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// ── Endpoint tests ──────────────────────────────────────────────────────

func TestAgentCard(t *testing.T) {
	cfg := baseConfig()
	cfg["provider"] = map[string]interface{}{
		"organization": "Acme Corp",
		"url":          "https://acme.com",
	}
	cfg["agent_capabilities"] = map[string]interface{}{
		"streaming":        true,
		"pushNotifications": false,
	}

	p := setupPlugin(t, cfg)
	rec := serveRequest(p, "/.well-known/agent.json")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var card A2AAgentCard
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &card))

	assert.Equal(t, "1.0.0", card.ProtocolVersion)
	assert.Equal(t, "Test API", card.Name)
	assert.Equal(t, "https://api.example.com", card.URL)
	assert.Equal(t, "1.0.0", card.Version)
	assert.Equal(t, []string{"text/plain"}, card.DefaultInputModes)
	assert.Equal(t, []string{"text/plain"}, card.DefaultOutputModes)
	require.Len(t, card.Skills, 1)
	assert.Equal(t, "widgets", card.Skills[0].ID)
	assert.Equal(t, "Widget Management", card.Skills[0].Name)
	assert.NotNil(t, card.Provider)
	assert.Equal(t, "Acme Corp", card.Provider.Organization)
	assert.NotNil(t, card.Capabilities)
	assert.True(t, card.Capabilities.Streaming)
}

func TestAgentCardAuth(t *testing.T) {
	cfg := baseConfig()
	cfg["auth"] = map[string]interface{}{
		"type": "api_key",
		"in":   "header",
		"name": "X-API-Key",
	}

	p := setupPlugin(t, cfg)
	rec := serveRequest(p, "/.well-known/agent.json")

	var card A2AAgentCard
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &card))

	require.NotNil(t, card.Authentication)
	assert.Equal(t, "apiKey", card.Authentication.Type) // api_key → apiKey for A2A
	assert.Equal(t, "header", card.Authentication.In)
	assert.Equal(t, "X-API-Key", card.Authentication.Name)
}

func TestLlmsTxt(t *testing.T) {
	p := setupPlugin(t, baseConfig())
	rec := serveRequest(p, "/llms.txt")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/plain")

	body := rec.Body.String()
	assert.Contains(t, body, "# Test API")
	assert.Contains(t, body, "> A test API")
	assert.Contains(t, body, "## Widget Management")
	assert.Contains(t, body, "CRUD operations for widgets")
	assert.Contains(t, body, "- List all widgets")
}

func TestLlmsTxtExtraSections(t *testing.T) {
	cfg := baseConfig()
	cfg["llms_txt_sections"] = []interface{}{
		map[string]interface{}{
			"title":   "Authentication",
			"content": "Use Bearer token in the Authorization header.",
		},
	}

	p := setupPlugin(t, cfg)
	rec := serveRequest(p, "/llms.txt")

	body := rec.Body.String()
	assert.Contains(t, body, "## Authentication")
	assert.Contains(t, body, "Use Bearer token")
}

func TestLlmsFullTxt(t *testing.T) {
	cfg := baseConfig()
	cfg["routes"] = []interface{}{
		map[string]interface{}{
			"method":  "GET",
			"path":    "/api/widgets",
			"summary": "List all widgets",
			"parameters": []interface{}{
				map[string]interface{}{
					"name":        "limit",
					"in":          "query",
					"required":    false,
					"description": "Max items to return",
				},
			},
		},
		map[string]interface{}{
			"method":      "POST",
			"path":        "/api/widgets",
			"summary":     "Create a widget",
			"description": "Creates a new widget resource.",
			"parameters": []interface{}{
				map[string]interface{}{
					"name":     "body",
					"in":       "body",
					"required": true,
				},
			},
		},
	}

	p := setupPlugin(t, cfg)
	rec := serveRequest(p, "/llms-full.txt")

	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "# Test API")
	assert.Contains(t, body, "## API Endpoints")
	assert.Contains(t, body, "### GET /api/widgets")
	assert.Contains(t, body, "List all widgets")
	assert.Contains(t, body, "`limit` (query)")
	assert.Contains(t, body, "Max items to return")
	assert.Contains(t, body, "### POST /api/widgets")
	assert.Contains(t, body, "(required)")
}

func TestAgentsTxtDefault(t *testing.T) {
	p := setupPlugin(t, baseConfig())
	rec := serveRequest(p, "/agents.txt")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/plain")

	body := rec.Body.String()
	assert.Contains(t, body, "User-agent: *")
	assert.Contains(t, body, "Allow: /")
	assert.Contains(t, body, "Test API")
}

func TestAgentsTxtCustom(t *testing.T) {
	cfg := baseConfig()
	cfg["agents_txt"] = map[string]interface{}{
		"comment": "Custom rules",
		"blocks": []interface{}{
			map[string]interface{}{
				"user_agent": "*",
				"rules": []interface{}{
					map[string]interface{}{"path": "/api/", "permission": "allow"},
					map[string]interface{}{"path": "/internal/", "permission": "disallow"},
				},
			},
			map[string]interface{}{
				"user_agent": "ClaudeBot",
				"rules": []interface{}{
					map[string]interface{}{"path": "/", "permission": "allow"},
				},
			},
		},
		"sitemap_url": "https://api.example.com/sitemap.xml",
	}

	p := setupPlugin(t, cfg)
	rec := serveRequest(p, "/agents.txt")

	body := rec.Body.String()
	assert.Contains(t, body, "# Custom rules")
	assert.Contains(t, body, "User-agent: *")
	assert.Contains(t, body, "Allow: /api/")
	assert.Contains(t, body, "Disallow: /internal/")
	assert.Contains(t, body, "User-agent: ClaudeBot")
	assert.Contains(t, body, "Sitemap: https://api.example.com/sitemap.xml")
}

// ── Non-discovery paths pass through ────────────────────────────────────

func TestNonDiscoveryPassThrough(t *testing.T) {
	p := setupPlugin(t, baseConfig())
	rec := serveRequest(p, "/api/widgets")

	assert.Equal(t, http.StatusTeapot, rec.Code)
	body, _ := io.ReadAll(rec.Result().Body)
	assert.Equal(t, "backend", string(body))
}

func TestRandomPathPassThrough(t *testing.T) {
	p := setupPlugin(t, baseConfig())

	for _, path := range []string{"/", "/health", "/api/v1/users", "/.well-known/openid-configuration"} {
		rec := serveRequest(p, path)
		assert.Equal(t, http.StatusTeapot, rec.Code, "path %s should pass through", path)
	}
}

// ── Disabled formats return 404 (pass through) ─────────────────────────

func TestDisabledFormats(t *testing.T) {
	cfg := baseConfig()
	cfg["formats"] = map[string]interface{}{
		"agent_card": false,
		"agents_txt": false,
		"llms_txt":   false,
	}

	p := setupPlugin(t, cfg)

	// All discovery paths should pass through to backend (418 Teapot)
	for _, path := range []string{"/.well-known/agent.json", "/agents.txt", "/llms.txt", "/llms-full.txt"} {
		rec := serveRequest(p, path)
		assert.Equal(t, http.StatusTeapot, rec.Code, "disabled path %s should pass through", path)
	}
}

func TestPartiallyDisabledFormats(t *testing.T) {
	cfg := baseConfig()
	cfg["formats"] = map[string]interface{}{
		"agent_card": false,
		"llms_txt":   true,
		"agents_txt": false,
	}

	p := setupPlugin(t, cfg)

	// Enabled
	assert.Equal(t, http.StatusOK, serveRequest(p, "/llms.txt").Code)
	assert.Equal(t, http.StatusOK, serveRequest(p, "/llms-full.txt").Code)

	// Disabled (pass through)
	assert.Equal(t, http.StatusTeapot, serveRequest(p, "/.well-known/agent.json").Code)
	assert.Equal(t, http.StatusTeapot, serveRequest(p, "/agents.txt").Code)
}

// ── Config reload regenerates endpoints ─────────────────────────────────

func TestReloadRegeneratesEndpoints(t *testing.T) {
	p := setupPlugin(t, baseConfig())

	// Initial name
	rec := serveRequest(p, "/.well-known/agent.json")
	var card A2AAgentCard
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &card))
	assert.Equal(t, "Test API", card.Name)

	// Reload with new name
	newCfg := baseConfig()
	newCfg["name"] = "Updated API"
	require.NoError(t, p.Reload(newCfg))

	rec = serveRequest(p, "/.well-known/agent.json")
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &card))
	assert.Equal(t, "Updated API", card.Name)
}

// ── Config validation ───────────────────────────────────────────────────

func TestInitRequiresName(t *testing.T) {
	p := New()
	err := p.Init(map[string]interface{}{
		"url": "https://example.com",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestInitRequiresURL(t *testing.T) {
	p := New()
	err := p.Init(map[string]interface{}{
		"name": "Test",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "url is required")
}

// ── Plugin interface compliance ─────────────────────────────────────────

func TestPluginName(t *testing.T) {
	p := New()
	assert.Equal(t, "discovery", p.Name())
}

func TestPluginClose(t *testing.T) {
	p := setupPlugin(t, baseConfig())
	assert.NoError(t, p.Close())
}

// ── Generator unit tests ────────────────────────────────────────────────

func TestGenerateAgentsTxtMultilineComment(t *testing.T) {
	cfg := &UnifiedDiscoveryConfig{
		Name: "Test",
		URL:  "https://example.com",
		AgentsTxt: &AgentsTxtConfig{
			Comment: "Line one\nLine two",
			Blocks: []AgentsTxtBlock{
				{UserAgent: "*", Rules: []AgentsTxtRule{{Path: "/", Permission: "allow"}}},
			},
		},
	}

	txt := generateAgentsTxt(cfg)
	assert.True(t, strings.HasPrefix(txt, "# Line one\n# Line two"))
}

func TestGenerateAgentCardDocURLFallback(t *testing.T) {
	cfg := &UnifiedDiscoveryConfig{
		Name:       "Test",
		URL:        "https://example.com",
		OpenAPIURL: "https://example.com/openapi.json",
	}

	card := generateAgentCard(cfg)
	assert.Equal(t, "https://example.com/openapi.json", card.DocumentationURL)
}

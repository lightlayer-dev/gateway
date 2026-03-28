package security

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── GenerateSecurityHeaders tests ──────────────────────────────────────

func TestGenerateSecurityHeaders_Defaults(t *testing.T) {
	headers := GenerateSecurityHeaders(SecurityHeadersConfig{
		HSTSMaxAge:            31536000,
		HSTSIncludeSubdomains: true,
		FrameOptions:          "DENY",
		ContentTypeOptions:    "nosniff",
		ReferrerPolicy:        "strict-origin-when-cross-origin",
		CSP:                   "default-src 'self'",
	})

	assert.Equal(t, "max-age=31536000; includeSubDomains", headers["Strict-Transport-Security"])
	assert.Equal(t, "nosniff", headers["X-Content-Type-Options"])
	assert.Equal(t, "DENY", headers["X-Frame-Options"])
	assert.Equal(t, "strict-origin-when-cross-origin", headers["Referrer-Policy"])
	assert.Equal(t, "default-src 'self'", headers["Content-Security-Policy"])
	_, hasPermissions := headers["Permissions-Policy"]
	assert.False(t, hasPermissions)
}

func TestGenerateSecurityHeaders_HSTSNoSubdomains(t *testing.T) {
	headers := GenerateSecurityHeaders(SecurityHeadersConfig{
		HSTSMaxAge:            3600,
		HSTSIncludeSubdomains: false,
	})
	assert.Equal(t, "max-age=3600", headers["Strict-Transport-Security"])
}

func TestGenerateSecurityHeaders_HSTSDisabled(t *testing.T) {
	headers := GenerateSecurityHeaders(SecurityHeadersConfig{
		HSTSMaxAge: 0,
	})
	_, has := headers["Strict-Transport-Security"]
	assert.False(t, has)
}

func TestGenerateSecurityHeaders_FrameOptionsSameorigin(t *testing.T) {
	headers := GenerateSecurityHeaders(SecurityHeadersConfig{
		FrameOptions: "SAMEORIGIN",
	})
	assert.Equal(t, "SAMEORIGIN", headers["X-Frame-Options"])
}

func TestGenerateSecurityHeaders_DisabledHeaders(t *testing.T) {
	headers := GenerateSecurityHeaders(SecurityHeadersConfig{})
	assert.Empty(t, headers)
}

func TestGenerateSecurityHeaders_PermissionsPolicy(t *testing.T) {
	headers := GenerateSecurityHeaders(SecurityHeadersConfig{
		PermissionsPolicy: "camera=(), microphone=()",
	})
	assert.Equal(t, "camera=(), microphone=()", headers["Permissions-Policy"])
}

// ── GenerateRobotsTxt tests ────────────────────────────────────────────

func TestGenerateRobotsTxt_DefaultAllowAIAgents(t *testing.T) {
	body := GenerateRobotsTxt(RobotsTxtConfig{
		IncludeAIAgents: true,
		AIAgentPolicy:   "allow",
		AIAllow:         []string{"/"},
	})

	assert.Contains(t, body, "User-agent: *\nAllow: /\n")
	for _, agent := range AIAgents {
		assert.Contains(t, body, "User-agent: "+agent)
		assert.Contains(t, body, "Allow: /")
	}
}

func TestGenerateRobotsTxt_DisallowAIAgents(t *testing.T) {
	body := GenerateRobotsTxt(RobotsTxtConfig{
		IncludeAIAgents: true,
		AIAgentPolicy:   "disallow",
	})

	for _, agent := range AIAgents {
		assert.Contains(t, body, "User-agent: "+agent+"\nDisallow: /")
	}
}

func TestGenerateRobotsTxt_ExplicitRules(t *testing.T) {
	body := GenerateRobotsTxt(RobotsTxtConfig{
		Rules: []RobotsTxtRule{
			{UserAgent: "GPTBot", Allow: []string{"/api/"}, Disallow: []string{"/admin/"}},
			{UserAgent: "*", Allow: []string{"/"}, CrawlDelay: 10},
		},
	})

	assert.Contains(t, body, "User-agent: GPTBot\nAllow: /api/\nDisallow: /admin/")
	assert.Contains(t, body, "User-agent: *\nAllow: /\nCrawl-delay: 10")
}

func TestGenerateRobotsTxt_Sitemaps(t *testing.T) {
	body := GenerateRobotsTxt(RobotsTxtConfig{
		IncludeAIAgents: false,
		Sitemaps:        []string{"https://example.com/sitemap.xml"},
	})

	assert.Contains(t, body, "Sitemap: https://example.com/sitemap.xml")
}

func TestGenerateRobotsTxt_NoAIAgents(t *testing.T) {
	body := GenerateRobotsTxt(RobotsTxtConfig{
		IncludeAIAgents: false,
	})

	assert.Contains(t, body, "User-agent: *\nAllow: /\n")
	for _, agent := range AIAgents {
		assert.NotContains(t, body, "User-agent: "+agent)
	}
}

func TestGenerateRobotsTxt_AIAllowDisallow(t *testing.T) {
	body := GenerateRobotsTxt(RobotsTxtConfig{
		IncludeAIAgents: true,
		AIAgentPolicy:   "allow",
		AIAllow:         []string{"/api/", "/docs/"},
		AIDisallow:      []string{"/internal/"},
	})

	// Check first agent block to verify structure.
	assert.Contains(t, body, "User-agent: GPTBot\nAllow: /api/\nAllow: /docs/\nDisallow: /internal/")
}

func TestAIAgentsConstant(t *testing.T) {
	expected := []string{
		"GPTBot", "ChatGPT-User", "Google-Extended", "Anthropic", "ClaudeBot",
		"CCBot", "Amazonbot", "Bytespider", "Applebot-Extended", "PerplexityBot", "Cohere-ai",
	}
	assert.Equal(t, expected, AIAgents)
}

// ── Middleware integration tests ───────────────────────────────────────

func newTestPlugin(t *testing.T, cfg map[string]interface{}) *Plugin {
	t.Helper()
	p := New()
	require.NoError(t, p.Init(cfg))
	return p
}

func TestMiddleware_SecurityHeadersOnResponse(t *testing.T) {
	p := newTestPlugin(t, map[string]interface{}{})
	handler := p.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "max-age=31536000; includeSubDomains", rec.Header().Get("Strict-Transport-Security"))
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
	assert.Equal(t, "strict-origin-when-cross-origin", rec.Header().Get("Referrer-Policy"))
	assert.Equal(t, "default-src 'self'", rec.Header().Get("Content-Security-Policy"))
}

func TestMiddleware_CORSRegularRequest(t *testing.T) {
	p := newTestPlugin(t, map[string]interface{}{
		"cors_origins": []interface{}{"https://example.com"},
	})
	handler := p.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "https://example.com", rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestMiddleware_CORSDisallowedOrigin(t *testing.T) {
	p := newTestPlugin(t, map[string]interface{}{
		"cors_origins": []interface{}{"https://example.com"},
	})
	handler := p.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Origin", "https://evil.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestMiddleware_CORSWildcard(t *testing.T) {
	p := newTestPlugin(t, map[string]interface{}{
		"cors_origins": []interface{}{"*"},
	})
	handler := p.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Origin", "https://anything.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, "https://anything.com", rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestMiddleware_CORSPreflight(t *testing.T) {
	p := newTestPlugin(t, map[string]interface{}{
		"cors_origins":     []interface{}{"https://example.com"},
		"cors_credentials": true,
		"cors_max_age":     3600,
	})
	handler := p.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not be called for preflight")
	}))

	req := httptest.NewRequest(http.MethodOptions, "/api/test", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "https://example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
	assert.Contains(t, rec.Header().Get("Access-Control-Allow-Methods"), "GET")
	assert.Contains(t, rec.Header().Get("Access-Control-Allow-Headers"), "Content-Type")
	assert.Equal(t, "3600", rec.Header().Get("Access-Control-Max-Age"))
}

func TestMiddleware_CORSPreflightNoMaxAge(t *testing.T) {
	p := newTestPlugin(t, map[string]interface{}{
		"cors_origins": []interface{}{"*"},
	})
	handler := p.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not be called for preflight")
	}))

	req := httptest.NewRequest(http.MethodOptions, "/api/test", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Header().Get("Access-Control-Max-Age"))
}

func TestMiddleware_RobotsTxtEndpoint(t *testing.T) {
	p := newTestPlugin(t, map[string]interface{}{})
	handler := p.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not be called for /robots.txt")
	}))

	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))

	body := rec.Body.String()
	assert.Contains(t, body, "User-agent: *")
	for _, agent := range AIAgents {
		assert.Contains(t, body, "User-agent: "+agent)
	}
}

func TestMiddleware_RobotsTxtWithSitemap(t *testing.T) {
	p := newTestPlugin(t, map[string]interface{}{
		"robots_txt": map[string]interface{}{
			"sitemaps": []interface{}{"https://example.com/sitemap.xml"},
		},
	})
	handler := p.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not be called for /robots.txt")
	}))

	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Contains(t, rec.Body.String(), "Sitemap: https://example.com/sitemap.xml")
}

func TestMiddleware_CustomHeaders(t *testing.T) {
	p := newTestPlugin(t, map[string]interface{}{
		"frame_options":      "SAMEORIGIN",
		"referrer_policy":    "no-referrer",
		"csp":                "default-src 'none'",
		"permissions_policy": "geolocation=()",
	})
	handler := p.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, "SAMEORIGIN", rec.Header().Get("X-Frame-Options"))
	assert.Equal(t, "no-referrer", rec.Header().Get("Referrer-Policy"))
	assert.Equal(t, "default-src 'none'", rec.Header().Get("Content-Security-Policy"))
	assert.Equal(t, "geolocation=()", rec.Header().Get("Permissions-Policy"))
}

func TestMiddleware_CORSCustomMethodsHeaders(t *testing.T) {
	p := newTestPlugin(t, map[string]interface{}{
		"cors_origins": []interface{}{"*"},
		"cors_methods": []interface{}{"GET", "POST"},
		"cors_headers": []interface{}{"X-Custom"},
	})
	handler := p.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodOptions, "/api/test", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	methods := rec.Header().Get("Access-Control-Allow-Methods")
	assert.Equal(t, "GET, POST", methods)
	assert.Equal(t, "X-Custom", rec.Header().Get("Access-Control-Allow-Headers"))
}

func TestPluginName(t *testing.T) {
	p := New()
	assert.Equal(t, "security", p.Name())
}

func TestPluginClose(t *testing.T) {
	p := New()
	assert.NoError(t, p.Close())
}

// ── Helper function tests ──────────────────────────────────────────────

func TestGenerateRobotsTxt_EndsWithNewline(t *testing.T) {
	body := GenerateRobotsTxt(RobotsTxtConfig{IncludeAIAgents: false})
	assert.True(t, strings.HasSuffix(body, "\n"))
}

func TestGenerateRobotsTxt_ExplicitRulesSkipAIAgents(t *testing.T) {
	body := GenerateRobotsTxt(RobotsTxtConfig{
		IncludeAIAgents: true,
		Rules: []RobotsTxtRule{
			{UserAgent: "Googlebot", Allow: []string{"/"}},
		},
	})

	assert.Contains(t, body, "User-agent: Googlebot")
	// When explicit rules are provided, AI agent rules are NOT auto-added
	// (matches TS behavior: includeAi && !config.rules).
	assert.NotContains(t, body, "User-agent: GPTBot")
}

package agentstxt

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lightlayer-dev/gateway/internal/detection"
	"github.com/lightlayer-dev/gateway/internal/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Generation Tests ────────────────────────────────────────────────────

func TestGenerateAgentsTxt(t *testing.T) {
	cfg := AgentsTxtConfig{
		SiteName:     "My API",
		Contact:      "support@example.com",
		DiscoveryURL: "https://example.com/.well-known/ai",
		Rules: []AgentsTxtRule{
			{
				Agent:              "*",
				Allow:              []string{"/api/public/*"},
				Deny:               []string{"/api/admin/*"},
				RateLimit:          &AgentsTxtRateLimit{Max: 100, WindowSeconds: 60},
				PreferredInterface: "rest",
				Description:        "Default rules for all agents",
			},
			{
				Agent: "ClaudeBot",
				Allow: []string{"/api/*", "/docs/*"},
				RateLimit: &AgentsTxtRateLimit{Max: 500, WindowSeconds: 60},
				Auth: &AgentsTxtAuth{
					Type:     "bearer",
					Endpoint: "https://example.com/oauth/token",
					DocsURL:  "https://docs.example.com/auth",
				},
			},
		},
	}

	content := GenerateAgentsTxt(cfg)

	assert.Contains(t, content, "# agents.txt — AI Agent Access Policy")
	assert.Contains(t, content, "# Site: My API")
	assert.Contains(t, content, "# Contact: support@example.com")
	assert.Contains(t, content, "# Discovery: https://example.com/.well-known/ai")
	assert.Contains(t, content, "User-agent: *")
	assert.Contains(t, content, "Allow: /api/public/*")
	assert.Contains(t, content, "Deny: /api/admin/*")
	assert.Contains(t, content, "Rate-limit: 100/60s")
	assert.Contains(t, content, "Preferred-interface: rest")
	assert.Contains(t, content, "# Default rules for all agents")
	assert.Contains(t, content, "User-agent: ClaudeBot")
	assert.Contains(t, content, "Allow: /api/*")
	assert.Contains(t, content, "Auth: bearer https://example.com/oauth/token")
	assert.Contains(t, content, "Auth-docs: https://docs.example.com/auth")
}

func TestGenerateMinimal(t *testing.T) {
	cfg := AgentsTxtConfig{
		Rules: []AgentsTxtRule{
			{Agent: "*"},
		},
	}
	content := GenerateAgentsTxt(cfg)
	assert.Contains(t, content, "# agents.txt — AI Agent Access Policy")
	assert.Contains(t, content, "User-agent: *")
}

// ── Parse Tests ─────────────────────────────────────────────────────────

func TestParseAgentsTxt(t *testing.T) {
	input := `# agents.txt — AI Agent Access Policy
# Site: Test API
# Contact: admin@test.com
# Discovery: https://test.com/.well-known/ai

User-agent: *
# General rules
Allow: /api/*
Deny: /internal/*
Rate-limit: 100/60s
Preferred-interface: rest

User-agent: ClaudeBot
Allow: /api/*
Allow: /docs/*
Rate-limit: 500/60s
Auth: bearer https://auth.test.com/token
Auth-docs: https://docs.test.com/auth
`

	cfg := ParseAgentsTxt(input)

	assert.Equal(t, "Test API", cfg.SiteName)
	assert.Equal(t, "admin@test.com", cfg.Contact)
	assert.Equal(t, "https://test.com/.well-known/ai", cfg.DiscoveryURL)
	require.Len(t, cfg.Rules, 2)

	// Wildcard rule.
	r0 := cfg.Rules[0]
	assert.Equal(t, "*", r0.Agent)
	assert.Equal(t, []string{"/api/*"}, r0.Allow)
	assert.Equal(t, []string{"/internal/*"}, r0.Deny)
	require.NotNil(t, r0.RateLimit)
	assert.Equal(t, 100, r0.RateLimit.Max)
	assert.Equal(t, 60, r0.RateLimit.WindowSeconds)
	assert.Equal(t, "rest", r0.PreferredInterface)

	// ClaudeBot rule.
	r1 := cfg.Rules[1]
	assert.Equal(t, "ClaudeBot", r1.Agent)
	assert.Equal(t, []string{"/api/*", "/docs/*"}, r1.Allow)
	require.NotNil(t, r1.RateLimit)
	assert.Equal(t, 500, r1.RateLimit.Max)
	require.NotNil(t, r1.Auth)
	assert.Equal(t, "bearer", r1.Auth.Type)
	assert.Equal(t, "https://auth.test.com/token", r1.Auth.Endpoint)
	assert.Equal(t, "https://docs.test.com/auth", r1.Auth.DocsURL)
}

func TestRoundTrip(t *testing.T) {
	original := AgentsTxtConfig{
		SiteName: "RoundTrip API",
		Contact:  "test@example.com",
		Rules: []AgentsTxtRule{
			{
				Agent:              "*",
				Allow:              []string{"/api/*"},
				Deny:               []string{"/admin/*"},
				RateLimit:          &AgentsTxtRateLimit{Max: 50, WindowSeconds: 30},
				PreferredInterface: "mcp",
			},
		},
	}

	content := GenerateAgentsTxt(original)
	parsed := ParseAgentsTxt(content)

	assert.Equal(t, original.SiteName, parsed.SiteName)
	assert.Equal(t, original.Contact, parsed.Contact)
	require.Len(t, parsed.Rules, 1)
	assert.Equal(t, original.Rules[0].Agent, parsed.Rules[0].Agent)
	assert.Equal(t, original.Rules[0].Allow, parsed.Rules[0].Allow)
	assert.Equal(t, original.Rules[0].Deny, parsed.Rules[0].Deny)
	assert.Equal(t, original.Rules[0].RateLimit.Max, parsed.Rules[0].RateLimit.Max)
	assert.Equal(t, original.Rules[0].RateLimit.WindowSeconds, parsed.Rules[0].RateLimit.WindowSeconds)
	assert.Equal(t, original.Rules[0].PreferredInterface, parsed.Rules[0].PreferredInterface)
}

// ── Rule Matching Tests ─────────────────────────────────────────────────

func TestIsAgentAllowed_ExactMatch(t *testing.T) {
	rules := []AgentsTxtRule{
		{Agent: "*", Allow: []string{"/api/*"}},
		{Agent: "ClaudeBot", Allow: []string{"/api/*", "/docs/*"}},
	}

	// ClaudeBot matches exact rule, gets /docs/*.
	result := IsAgentAllowed(rules, "ClaudeBot", "/docs/readme")
	require.NotNil(t, result)
	assert.True(t, *result)

	// Other agent hits wildcard, no /docs access.
	result = IsAgentAllowed(rules, "GPTBot", "/docs/readme")
	require.NotNil(t, result)
	assert.False(t, *result)
}

func TestIsAgentAllowed_PrefixPattern(t *testing.T) {
	rules := []AgentsTxtRule{
		{Agent: "*", Allow: []string{"/api/*"}},
		{Agent: "GPT*", Allow: []string{"/api/*", "/premium/*"}},
	}

	// GPTBot matches GPT* pattern.
	result := IsAgentAllowed(rules, "GPTBot", "/premium/data")
	require.NotNil(t, result)
	assert.True(t, *result)

	// GPT-4 also matches.
	result = IsAgentAllowed(rules, "GPT-4", "/premium/data")
	require.NotNil(t, result)
	assert.True(t, *result)

	// ClaudeBot falls back to wildcard.
	result = IsAgentAllowed(rules, "ClaudeBot", "/premium/data")
	require.NotNil(t, result)
	assert.False(t, *result)
}

func TestIsAgentAllowed_DenyTakesPrecedence(t *testing.T) {
	rules := []AgentsTxtRule{
		{
			Agent: "*",
			Allow: []string{"/api/*"},
			Deny:  []string{"/api/admin/*"},
		},
	}

	// /api/public is allowed.
	result := IsAgentAllowed(rules, "AnyBot", "/api/public")
	require.NotNil(t, result)
	assert.True(t, *result)

	// /api/admin/users is denied (deny takes precedence).
	result = IsAgentAllowed(rules, "AnyBot", "/api/admin/users")
	require.NotNil(t, result)
	assert.False(t, *result)
}

func TestIsAgentAllowed_NoMatchingRule(t *testing.T) {
	rules := []AgentsTxtRule{
		{Agent: "ClaudeBot", Allow: []string{"/api/*"}},
	}

	// No wildcard rule, unrecognized agent returns nil.
	result := IsAgentAllowed(rules, "GPTBot", "/api/test")
	assert.Nil(t, result)
}

func TestIsAgentAllowed_NoAllowDeny_ImplicitAllow(t *testing.T) {
	rules := []AgentsTxtRule{
		{Agent: "*", Description: "All agents welcome"},
	}

	result := IsAgentAllowed(rules, "AnyBot", "/anything")
	require.NotNil(t, result)
	assert.True(t, *result)
}

func TestIsAgentAllowed_AllowExistsButNoMatch_Deny(t *testing.T) {
	rules := []AgentsTxtRule{
		{Agent: "*", Allow: []string{"/api/*"}},
	}

	// Path doesn't match any allow pattern → denied.
	result := IsAgentAllowed(rules, "AnyBot", "/secret/data")
	require.NotNil(t, result)
	assert.False(t, *result)
}

// ── Path Matching Tests ─────────────────────────────────────────────────

func TestPathMatches(t *testing.T) {
	tests := []struct {
		path    string
		pattern string
		want    bool
	}{
		{"/anything", "*", true},
		{"/anything", "/*", true},
		{"/api/test", "/api/*", true},
		{"/api/nested/deep", "/api/*", true},
		{"/other", "/api/*", false},
		{"/api/test", "/api/test", true},
		{"/api/test2", "/api/test", false},
	}

	for _, tt := range tests {
		t.Run(tt.path+"_"+tt.pattern, func(t *testing.T) {
			assert.Equal(t, tt.want, pathMatches(tt.path, tt.pattern))
		})
	}
}

// ── Middleware / Endpoint Tests ──────────────────────────────────────────

func makePluginRequest(p *Plugin, method, path string, agentInfo *detection.AgentInfo) *httptest.ResponseRecorder {
	handler := p.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(method, path, nil)
	rc := &plugins.RequestContext{
		RequestID: "test-123",
		StartTime: time.Now(),
		AgentInfo: agentInfo,
		Metadata:  make(map[string]interface{}),
	}
	ctx := plugins.WithRequestContext(req.Context(), rc)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestServesAgentsTxtEndpoint(t *testing.T) {
	p := New()
	err := p.Init(map[string]interface{}{
		"site_name": "Test API",
		"rules": []interface{}{
			map[string]interface{}{
				"agent": "*",
				"allow": []interface{}{"/api/*"},
			},
		},
	})
	require.NoError(t, err)

	rr := makePluginRequest(p, http.MethodGet, "/agents.txt", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "text/plain; charset=utf-8", rr.Header().Get("Content-Type"))
	assert.Contains(t, rr.Body.String(), "# agents.txt — AI Agent Access Policy")
	assert.Contains(t, rr.Body.String(), "# Site: Test API")
	assert.Contains(t, rr.Body.String(), "User-agent: *")
	assert.Contains(t, rr.Body.String(), "Allow: /api/*")
}

func TestEnforcesAccessRules(t *testing.T) {
	p := New()
	err := p.Init(map[string]interface{}{
		"rules": []interface{}{
			map[string]interface{}{
				"agent": "*",
				"allow": []interface{}{"/api/*"},
				"deny":  []interface{}{"/api/admin/*"},
			},
		},
	})
	require.NoError(t, err)

	claude := &detection.AgentInfo{Detected: true, Name: "ClaudeBot", Provider: "Anthropic"}

	// Allowed path.
	rr := makePluginRequest(p, http.MethodGet, "/api/widgets", claude)
	assert.Equal(t, http.StatusOK, rr.Code)

	// Denied path.
	rr = makePluginRequest(p, http.MethodGet, "/api/admin/users", claude)
	assert.Equal(t, http.StatusForbidden, rr.Code)

	var env plugins.AgentErrorEnvelope
	err = json.NewDecoder(rr.Body).Decode(&env)
	require.NoError(t, err)
	assert.Equal(t, "permission_error", env.Type)
	assert.Equal(t, "agent_denied", env.Code)

	// Path not matching any allow → denied.
	rr = makePluginRequest(p, http.MethodGet, "/secret/data", claude)
	assert.Equal(t, http.StatusForbidden, rr.Code)
}

func TestNoRulesPassesThrough(t *testing.T) {
	p := New()
	err := p.Init(map[string]interface{}{})
	require.NoError(t, err)

	rr := makePluginRequest(p, http.MethodGet, "/anything", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestUnknownAgentNoWildcard(t *testing.T) {
	p := New()
	err := p.Init(map[string]interface{}{
		"rules": []interface{}{
			map[string]interface{}{
				"agent": "ClaudeBot",
				"allow": []interface{}{"/api/*"},
			},
		},
	})
	require.NoError(t, err)

	// GPTBot doesn't match ClaudeBot and no wildcard → passes through.
	gpt := &detection.AgentInfo{Detected: true, Name: "GPTBot", Provider: "OpenAI"}
	rr := makePluginRequest(p, http.MethodGet, "/api/test", gpt)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestPluginName(t *testing.T) {
	p := New()
	assert.Equal(t, "agents_txt", p.Name())
}

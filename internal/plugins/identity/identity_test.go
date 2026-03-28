package identity

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lightlayer-dev/gateway/internal/detection"
	"github.com/lightlayer-dev/gateway/internal/plugins"
)

// ── SPIFFE ID Parsing ────────────────────────────────────────────────────

func TestParseSpiffeId(t *testing.T) {
	tests := []struct {
		name   string
		uri    string
		want   *SpiffeId
	}{
		{
			name: "valid with path",
			uri:  "spiffe://example.com/workload/agent-1",
			want: &SpiffeId{TrustDomain: "example.com", Path: "/workload/agent-1", Raw: "spiffe://example.com/workload/agent-1"},
		},
		{
			name: "valid without path",
			uri:  "spiffe://example.com",
			want: &SpiffeId{TrustDomain: "example.com", Path: "/", Raw: "spiffe://example.com"},
		},
		{
			name: "valid root path",
			uri:  "spiffe://trust.domain/",
			want: &SpiffeId{TrustDomain: "trust.domain", Path: "/", Raw: "spiffe://trust.domain/"},
		},
		{
			name: "not a spiffe URI",
			uri:  "https://example.com/foo",
			want: nil,
		},
		{
			name: "empty string",
			uri:  "",
			want: nil,
		},
		{
			name: "plain identifier",
			uri:  "agent-123",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSpiffeId(tt.uri)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsSpiffeTrusted(t *testing.T) {
	id := &SpiffeId{TrustDomain: "acme.com", Path: "/agent", Raw: "spiffe://acme.com/agent"}

	assert.True(t, IsSpiffeTrusted(id, []string{"acme.com", "other.com"}))
	assert.False(t, IsSpiffeTrusted(id, []string{"other.com"}))
	assert.False(t, IsSpiffeTrusted(id, []string{}))
}

// ── Claims Extraction ────────────────────────────────────────────────────

func TestExtractClaims(t *testing.T) {
	t.Run("basic claims", func(t *testing.T) {
		payload := map[string]interface{}{
			"iss":      "https://auth.example.com",
			"sub":      "agent-42",
			"aud":      "https://api.example.com",
			"exp":      float64(1700000000),
			"iat":      float64(1699999000),
			"scope":    "read write",
			"custom_x": "hello",
		}

		claims := ExtractClaims(payload)
		assert.Equal(t, "agent-42", claims.AgentId)
		assert.Equal(t, "https://auth.example.com", claims.Issuer)
		assert.Equal(t, "agent-42", claims.Subject)
		assert.Equal(t, []string{"https://api.example.com"}, claims.Audience)
		assert.Equal(t, int64(1700000000), claims.ExpiresAt)
		assert.Equal(t, int64(1699999000), claims.IssuedAt)
		assert.Equal(t, []string{"read", "write"}, claims.Scopes)
		assert.False(t, claims.Delegated)
		assert.Equal(t, "", claims.DelegatedBy)
		assert.Equal(t, "hello", claims.CustomClaims["custom_x"])
		assert.Nil(t, claims.SpiffeId)
	})

	t.Run("SPIFFE agent ID", func(t *testing.T) {
		payload := map[string]interface{}{
			"iss":      "https://auth.example.com",
			"sub":      "spiffe://acme.com/agent/bot-1",
			"aud":      []interface{}{"aud1", "aud2"},
			"exp":      float64(1700000000),
			"iat":      float64(1699999000),
		}

		claims := ExtractClaims(payload)
		assert.Equal(t, "spiffe://acme.com/agent/bot-1", claims.AgentId)
		require.NotNil(t, claims.SpiffeId)
		assert.Equal(t, "acme.com", claims.SpiffeId.TrustDomain)
		assert.Equal(t, "/agent/bot-1", claims.SpiffeId.Path)
		assert.Equal(t, []string{"aud1", "aud2"}, claims.Audience)
	})

	t.Run("explicit agent_id takes precedence", func(t *testing.T) {
		payload := map[string]interface{}{
			"iss":      "issuer",
			"sub":      "subject",
			"agent_id": "my-agent",
		}
		claims := ExtractClaims(payload)
		assert.Equal(t, "my-agent", claims.AgentId)
	})

	t.Run("delegated access", func(t *testing.T) {
		payload := map[string]interface{}{
			"iss": "issuer",
			"sub": "agent-1",
			"act": map[string]interface{}{
				"sub": "user@example.com",
			},
		}
		claims := ExtractClaims(payload)
		assert.True(t, claims.Delegated)
		assert.Equal(t, "user@example.com", claims.DelegatedBy)
	})

	t.Run("scopes from array", func(t *testing.T) {
		payload := map[string]interface{}{
			"iss":    "issuer",
			"sub":    "agent-1",
			"scopes": []interface{}{"admin", "read"},
		}
		claims := ExtractClaims(payload)
		assert.Equal(t, []string{"admin", "read"}, claims.Scopes)
	})

	t.Run("scopes from scp", func(t *testing.T) {
		payload := map[string]interface{}{
			"iss": "issuer",
			"sub": "agent-1",
			"scp": []interface{}{"api:read"},
		}
		claims := ExtractClaims(payload)
		assert.Equal(t, []string{"api:read"}, claims.Scopes)
	})
}

// ── Token Validation ─────────────────────────────────────────────────────

func TestValidateClaims(t *testing.T) {
	now := time.Now().Unix()
	baseCfg := &identityConfig{
		TrustedIssuers:     []string{"https://auth.example.com"},
		Audience:           []string{"https://api.example.com"},
		ClockSkewSeconds:   30,
		MaxLifetimeSeconds: 3600,
	}

	t.Run("valid claims", func(t *testing.T) {
		claims := &AgentIdentityClaims{
			Issuer:   "https://auth.example.com",
			Audience: []string{"https://api.example.com"},
			IssuedAt:  now - 60,
			ExpiresAt: now + 300,
		}
		assert.Nil(t, ValidateClaims(claims, baseCfg))
	})

	t.Run("untrusted issuer", func(t *testing.T) {
		claims := &AgentIdentityClaims{
			Issuer:   "https://evil.com",
			Audience: []string{"https://api.example.com"},
			IssuedAt:  now - 60,
			ExpiresAt: now + 300,
		}
		err := ValidateClaims(claims, baseCfg)
		require.NotNil(t, err)
		assert.Equal(t, "untrusted_issuer", err.Code)
	})

	t.Run("invalid audience", func(t *testing.T) {
		claims := &AgentIdentityClaims{
			Issuer:   "https://auth.example.com",
			Audience: []string{"https://other.com"},
			IssuedAt:  now - 60,
			ExpiresAt: now + 300,
		}
		err := ValidateClaims(claims, baseCfg)
		require.NotNil(t, err)
		assert.Equal(t, "invalid_audience", err.Code)
	})

	t.Run("expired token", func(t *testing.T) {
		claims := &AgentIdentityClaims{
			Issuer:   "https://auth.example.com",
			Audience: []string{"https://api.example.com"},
			IssuedAt:  now - 7200,
			ExpiresAt: now - 60,
		}
		err := ValidateClaims(claims, baseCfg)
		require.NotNil(t, err)
		assert.Equal(t, "expired_token", err.Code)
	})

	t.Run("token too long lived", func(t *testing.T) {
		claims := &AgentIdentityClaims{
			Issuer:   "https://auth.example.com",
			Audience: []string{"https://api.example.com"},
			IssuedAt:  now - 100,
			ExpiresAt: now + 7200, // lifetime = 7300s > 3600s
		}
		err := ValidateClaims(claims, baseCfg)
		require.NotNil(t, err)
		assert.Equal(t, "token_too_long_lived", err.Code)
	})

	t.Run("untrusted SPIFFE domain", func(t *testing.T) {
		cfg := &identityConfig{
			TrustedIssuers:     []string{"https://auth.example.com"},
			ClockSkewSeconds:   30,
			MaxLifetimeSeconds: 3600,
			TrustedDomains:     []string{"trusted.com"},
		}
		claims := &AgentIdentityClaims{
			Issuer:   "https://auth.example.com",
			IssuedAt:  now - 60,
			ExpiresAt: now + 300,
			SpiffeId: &SpiffeId{TrustDomain: "evil.com", Path: "/agent", Raw: "spiffe://evil.com/agent"},
		}
		err := ValidateClaims(claims, cfg)
		require.NotNil(t, err)
		assert.Equal(t, "untrusted_domain", err.Code)
	})

	t.Run("clock skew tolerance", func(t *testing.T) {
		claims := &AgentIdentityClaims{
			Issuer:   "https://auth.example.com",
			Audience: []string{"https://api.example.com"},
			IssuedAt:  now - 300,
			ExpiresAt: now - 10, // expired 10s ago, but within 30s skew
		}
		assert.Nil(t, ValidateClaims(claims, baseCfg))
	})
}

// ── Glob Matching ────────────────────────────────────────────────────────

func TestGlobMatch(t *testing.T) {
	assert.True(t, GlobMatch("*", "anything"))
	assert.True(t, GlobMatch("agent-*", "agent-123"))
	assert.False(t, GlobMatch("agent-*", "bot-123"))
	assert.True(t, GlobMatch("/api/*", "/api/users"))
	assert.True(t, GlobMatch("/api/*/items", "/api/v1/items"))
	assert.False(t, GlobMatch("/api/v1", "/api/v2"))
	assert.True(t, GlobMatch("spiffe://acme.com/*", "spiffe://acme.com/agent/bot-1"))
}

// ── Authorization ────────────────────────────────────────────────────────

func TestEvaluateAuthz(t *testing.T) {
	t.Run("matching policy allows access", func(t *testing.T) {
		policies := []AgentAuthzPolicy{
			{
				Name:         "allow-readers",
				AgentPattern: "agent-*",
				Methods:      []string{"GET"},
				Paths:        []string{"/api/*"},
			},
		}
		claims := &AgentIdentityClaims{AgentId: "agent-42", Scopes: []string{"read"}}
		ctx := AuthzContext{Method: "GET", Path: "/api/users"}
		result := EvaluateAuthz(claims, ctx, policies, "deny")
		assert.True(t, result.Allowed)
		assert.Equal(t, "allow-readers", result.MatchedPolicy)
	})

	t.Run("no matching policy uses default deny", func(t *testing.T) {
		policies := []AgentAuthzPolicy{
			{
				Name:    "admin-only",
				Methods: []string{"DELETE"},
			},
		}
		claims := &AgentIdentityClaims{AgentId: "agent-42"}
		ctx := AuthzContext{Method: "GET", Path: "/api/users"}
		result := EvaluateAuthz(claims, ctx, policies, "deny")
		assert.False(t, result.Allowed)
	})

	t.Run("no matching policy uses default allow", func(t *testing.T) {
		policies := []AgentAuthzPolicy{
			{
				Name:    "admin-only",
				Methods: []string{"DELETE"},
			},
		}
		claims := &AgentIdentityClaims{AgentId: "agent-42"}
		ctx := AuthzContext{Method: "GET", Path: "/api/users"}
		result := EvaluateAuthz(claims, ctx, policies, "allow")
		assert.True(t, result.Allowed)
	})

	t.Run("missing required scopes denied", func(t *testing.T) {
		policies := []AgentAuthzPolicy{
			{
				Name:           "needs-write",
				RequiredScopes: []string{"write", "admin"},
			},
		}
		claims := &AgentIdentityClaims{AgentId: "agent-42", Scopes: []string{"write"}}
		ctx := AuthzContext{Method: "POST", Path: "/api/data"}
		result := EvaluateAuthz(claims, ctx, policies, "deny")
		assert.False(t, result.Allowed)
		assert.Contains(t, result.DeniedReason, "admin")
	})

	t.Run("delegated access denied", func(t *testing.T) {
		deny := false
		policies := []AgentAuthzPolicy{
			{
				Name:           "no-delegation",
				AllowDelegated: &deny,
			},
		}
		claims := &AgentIdentityClaims{AgentId: "agent-42", Delegated: true, DelegatedBy: "user@example.com"}
		ctx := AuthzContext{Method: "GET", Path: "/"}
		result := EvaluateAuthz(claims, ctx, policies, "deny")
		assert.False(t, result.Allowed)
		assert.Contains(t, result.DeniedReason, "Delegated")
	})

	t.Run("trust domain matching", func(t *testing.T) {
		policies := []AgentAuthzPolicy{
			{
				Name:         "acme-only",
				TrustDomains: []string{"acme.com"},
			},
		}
		claims := &AgentIdentityClaims{
			AgentId:  "spiffe://acme.com/agent",
			SpiffeId: &SpiffeId{TrustDomain: "acme.com", Path: "/agent", Raw: "spiffe://acme.com/agent"},
		}
		ctx := AuthzContext{Method: "GET", Path: "/"}
		result := EvaluateAuthz(claims, ctx, policies, "deny")
		assert.True(t, result.Allowed)
	})
}

// ── Audit Event ──────────────────────────────────────────────────────────

func TestBuildAuditEvent(t *testing.T) {
	claims := &AgentIdentityClaims{
		AgentId:   "agent-42",
		Issuer:    "https://auth.example.com",
		Scopes:    []string{"read"},
		Delegated: false,
		SpiffeId:  &SpiffeId{TrustDomain: "acme.com", Path: "/agent", Raw: "spiffe://acme.com/agent"},
	}
	ctx := AuthzContext{Method: "GET", Path: "/api/data"}
	result := AuthzResult{Allowed: true, MatchedPolicy: "default"}

	event := BuildAuditEvent(claims, ctx, result)
	assert.Equal(t, "agent_identity", event.Type)
	assert.Equal(t, "agent-42", event.AgentId)
	assert.Equal(t, "spiffe://acme.com/agent", event.SpiffeId)
	assert.Equal(t, "GET", event.Method)
	assert.True(t, event.AuthzResult.Allowed)
	assert.NotEmpty(t, event.Timestamp)
}

// ── Middleware Integration Tests ─────────────────────────────────────────

// makeTestJWT creates a minimal unsigned JWT for testing.
// The token has the correct 3-part structure with base64url-encoded header and payload.
func makeTestJWT(claims map[string]interface{}) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, _ := json.Marshal(claims)
	payloadEnc := base64.RawURLEncoding.EncodeToString(payload)
	return fmt.Sprintf("%s.%s.", header, payloadEnc)
}

func TestMiddleware_EnforceMode_ValidToken(t *testing.T) {
	p := New()
	now := time.Now().Unix()

	err := p.Init(map[string]interface{}{
		"mode":             "enforce",
		"trusted_issuers":  []interface{}{"https://auth.example.com"},
		"audience":         []interface{}{"https://api.example.com"},
		"clock_skew_seconds":   30,
		"max_lifetime_seconds": 3600,
	})
	require.NoError(t, err)

	token := makeTestJWT(map[string]interface{}{
		"iss": "https://auth.example.com",
		"sub": "agent-42",
		"aud": "https://api.example.com",
		"exp": float64(now + 300),
		"iat": float64(now - 60),
	})

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		// Verify headers are set on the request
		assert.Equal(t, "agent-42", r.Header.Get("X-Agent-Id"))
		assert.Equal(t, "true", r.Header.Get("X-Agent-Verified"))
		w.WriteHeader(http.StatusOK)
	})

	handler := p.Middleware()(next)

	req := httptest.NewRequest("GET", "/api/data", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	// Inject RequestContext so the plugin can set metadata.
	rc := &plugins.RequestContext{
		Metadata:  make(map[string]interface{}),
		AgentInfo: &detection.AgentInfo{Detected: true, Name: "test-agent"},
	}
	req = req.WithContext(plugins.WithRequestContext(req.Context(), rc))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.True(t, nextCalled)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "agent-42", rr.Header().Get("X-Agent-Id"))
	assert.Equal(t, "true", rr.Header().Get("X-Agent-Verified"))

	// Verify metadata was set.
	assert.True(t, rc.Metadata["identity_verified"].(bool))
	assert.True(t, rc.AgentInfo.Verified)
}

func TestMiddleware_EnforceMode_MissingToken(t *testing.T) {
	p := New()
	err := p.Init(map[string]interface{}{
		"mode":            "enforce",
		"trusted_issuers": []interface{}{"https://auth.example.com"},
	})
	require.NoError(t, err)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next should not be called")
	})

	handler := p.Middleware()(next)
	req := httptest.NewRequest("GET", "/api/data", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Body.String(), "missing_token")
}

func TestMiddleware_EnforceMode_ExpiredToken(t *testing.T) {
	p := New()
	now := time.Now().Unix()

	err := p.Init(map[string]interface{}{
		"mode":             "enforce",
		"trusted_issuers":  []interface{}{"https://auth.example.com"},
		"clock_skew_seconds": 30,
		"max_lifetime_seconds": 3600,
	})
	require.NoError(t, err)

	token := makeTestJWT(map[string]interface{}{
		"iss": "https://auth.example.com",
		"sub": "agent-42",
		"exp": float64(now - 120), // expired 120s ago
		"iat": float64(now - 600),
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next should not be called")
	})

	handler := p.Middleware()(next)
	req := httptest.NewRequest("GET", "/api/data", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Body.String(), "expired_token")
}

func TestMiddleware_LogMode_ExpiredToken(t *testing.T) {
	p := New()
	now := time.Now().Unix()

	err := p.Init(map[string]interface{}{
		"mode":             "log",
		"trusted_issuers":  []interface{}{"https://auth.example.com"},
		"clock_skew_seconds": 30,
		"max_lifetime_seconds": 3600,
	})
	require.NoError(t, err)

	token := makeTestJWT(map[string]interface{}{
		"iss": "https://auth.example.com",
		"sub": "agent-42",
		"exp": float64(now - 120), // expired
		"iat": float64(now - 600),
	})

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := p.Middleware()(next)
	req := httptest.NewRequest("GET", "/api/data", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.True(t, nextCalled, "log mode should pass through expired tokens")
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestMiddleware_WarnMode_ExpiredToken(t *testing.T) {
	p := New()
	now := time.Now().Unix()

	err := p.Init(map[string]interface{}{
		"mode":             "warn",
		"trusted_issuers":  []interface{}{"https://auth.example.com"},
		"clock_skew_seconds": 30,
		"max_lifetime_seconds": 3600,
	})
	require.NoError(t, err)

	token := makeTestJWT(map[string]interface{}{
		"iss": "https://auth.example.com",
		"sub": "agent-42",
		"exp": float64(now - 120), // expired
		"iat": float64(now - 600),
	})

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := p.Middleware()(next)
	req := httptest.NewRequest("GET", "/api/data", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.True(t, nextCalled, "warn mode should pass through expired tokens")
	assert.Equal(t, "false", rr.Header().Get("X-Agent-Verified"))
	assert.Equal(t, "expired_token", rr.Header().Get("X-Agent-Identity-Error"))
}

func TestMiddleware_LogMode_NoToken(t *testing.T) {
	p := New()
	err := p.Init(map[string]interface{}{
		"mode": "log",
	})
	require.NoError(t, err)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := p.Middleware()(next)
	req := httptest.NewRequest("GET", "/api/data", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.True(t, nextCalled, "log mode should pass through without token")
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestMiddleware_WarnMode_NoToken(t *testing.T) {
	p := New()
	err := p.Init(map[string]interface{}{
		"mode": "warn",
	})
	require.NoError(t, err)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := p.Middleware()(next)
	req := httptest.NewRequest("GET", "/api/data", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.True(t, nextCalled)
	assert.Equal(t, "false", rr.Header().Get("X-Agent-Verified"))
}

func TestMiddleware_EnforceMode_AuthzDenied(t *testing.T) {
	p := New()
	now := time.Now().Unix()

	err := p.Init(map[string]interface{}{
		"mode":            "enforce",
		"trusted_issuers": []interface{}{"https://auth.example.com"},
		"default_policy":  "deny",
		"max_lifetime_seconds": 3600,
		"clock_skew_seconds":   30,
		"policies": []interface{}{
			map[string]interface{}{
				"name":            "admin-only",
				"required_scopes": []interface{}{"admin"},
			},
		},
	})
	require.NoError(t, err)

	token := makeTestJWT(map[string]interface{}{
		"iss":   "https://auth.example.com",
		"sub":   "agent-42",
		"exp":   float64(now + 300),
		"iat":   float64(now - 60),
		"scope": "read",
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next should not be called")
	})

	handler := p.Middleware()(next)
	req := httptest.NewRequest("GET", "/api/data", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
	assert.Contains(t, rr.Body.String(), "authorization_denied")
}

func TestMiddleware_MalformedToken(t *testing.T) {
	p := New()
	err := p.Init(map[string]interface{}{
		"mode": "enforce",
	})
	require.NoError(t, err)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next should not be called")
	})

	handler := p.Middleware()(next)
	req := httptest.NewRequest("GET", "/api/data", nil)
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Body.String(), "malformed_token")
}

func TestMiddleware_UntrustedIssuer_Enforce(t *testing.T) {
	p := New()
	now := time.Now().Unix()

	err := p.Init(map[string]interface{}{
		"mode":            "enforce",
		"trusted_issuers": []interface{}{"https://trusted.com"},
		"max_lifetime_seconds": 3600,
		"clock_skew_seconds":   30,
	})
	require.NoError(t, err)

	token := makeTestJWT(map[string]interface{}{
		"iss": "https://evil.com",
		"sub": "agent-42",
		"exp": float64(now + 300),
		"iat": float64(now - 60),
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next should not be called")
	})

	handler := p.Middleware()(next)
	req := httptest.NewRequest("GET", "/api/data", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
	assert.Contains(t, rr.Body.String(), "untrusted_issuer")
}

// ── Config Parsing ──────────────────────────────────────────────────────

func TestParseConfig_Defaults(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, "log", cfg.Mode)
	assert.Equal(t, "Authorization", cfg.HeaderName)
	assert.Equal(t, "Bearer", cfg.TokenPrefix)
	assert.Equal(t, 30, cfg.ClockSkewSeconds)
	assert.Equal(t, 3600, cfg.MaxLifetimeSeconds)
	assert.Equal(t, "deny", cfg.DefaultPolicy)
}

func TestParseConfig_CustomValues(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"mode":                 "enforce",
		"header_name":          "X-Agent-Token",
		"token_prefix":         "Token",
		"clock_skew_seconds":   60,
		"max_lifetime_seconds": 7200,
		"default_policy":       "allow",
	})
	require.NoError(t, err)
	assert.Equal(t, "enforce", cfg.Mode)
	assert.Equal(t, "X-Agent-Token", cfg.HeaderName)
	assert.Equal(t, "Token", cfg.TokenPrefix)
	assert.Equal(t, 60, cfg.ClockSkewSeconds)
	assert.Equal(t, 7200, cfg.MaxLifetimeSeconds)
	assert.Equal(t, "allow", cfg.DefaultPolicy)
}

// ── Token Extraction ────────────────────────────────────────────────────

func TestExtractToken(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		headerName string
		prefix     string
		value      string
		want       string
	}{
		{"bearer token", "Authorization", "Authorization", "Bearer", "Bearer abc123", "abc123"},
		{"no prefix", "X-Token", "X-Token", "", "abc123", "abc123"},
		{"wrong prefix", "Authorization", "Authorization", "Bearer", "Token abc123", ""},
		{"empty header", "Authorization", "Authorization", "Bearer", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			if tt.value != "" {
				r.Header.Set(tt.headerName, tt.value)
			}
			got := extractToken(r, tt.headerName, tt.prefix)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ── Plugin Registration ─────────────────────────────────────────────────

func TestPluginRegistered(t *testing.T) {
	ctor := plugins.GetConstructor("identity")
	require.NotNil(t, ctor)
	p := ctor()
	assert.Equal(t, "identity", p.Name())
}

// ── extractToken with custom header ─────────────────────────────────────

func TestMiddleware_CustomHeader(t *testing.T) {
	p := New()
	now := time.Now().Unix()

	err := p.Init(map[string]interface{}{
		"mode":                 "enforce",
		"trusted_issuers":      []interface{}{"https://auth.example.com"},
		"header_name":          "X-Agent-Token",
		"token_prefix":         "",
		"max_lifetime_seconds": 3600,
		"clock_skew_seconds":   30,
	})
	require.NoError(t, err)

	token := makeTestJWT(map[string]interface{}{
		"iss": "https://auth.example.com",
		"sub": "agent-99",
		"exp": float64(now + 300),
		"iat": float64(now - 60),
	})

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		assert.Equal(t, "agent-99", r.Header.Get("X-Agent-Id"))
		w.WriteHeader(http.StatusOK)
	})

	handler := p.Middleware()(next)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Agent-Token", token)

	rc := &plugins.RequestContext{
		Metadata: make(map[string]interface{}),
	}
	req = req.WithContext(plugins.WithRequestContext(req.Context(), rc))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.True(t, nextCalled)
}

// ── SPIFFE ID in middleware ──────────────────────────────────────────────

func TestMiddleware_SpiffeId_SetOnRequest(t *testing.T) {
	p := New()
	now := time.Now().Unix()

	err := p.Init(map[string]interface{}{
		"mode":                 "enforce",
		"trusted_issuers":      []interface{}{"https://auth.example.com"},
		"trusted_domains":      []interface{}{"acme.com"},
		"max_lifetime_seconds": 3600,
		"clock_skew_seconds":   30,
	})
	require.NoError(t, err)

	token := makeTestJWT(map[string]interface{}{
		"iss": "https://auth.example.com",
		"sub": "spiffe://acme.com/agents/bot-1",
		"exp": float64(now + 300),
		"iat": float64(now - 60),
	})

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		assert.Equal(t, "spiffe://acme.com/agents/bot-1", r.Header.Get("X-Agent-Id"))
		w.WriteHeader(http.StatusOK)
	})

	handler := p.Middleware()(next)
	req := httptest.NewRequest("GET", "/api/data", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rc := &plugins.RequestContext{
		Metadata: make(map[string]interface{}),
	}
	req = req.WithContext(plugins.WithRequestContext(req.Context(), rc))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.True(t, nextCalled)
	assert.Equal(t, "spiffe://acme.com/agents/bot-1", rr.Header().Get("X-Agent-Id"))

	// Verify SPIFFE ID stored in context.
	claims := rc.Metadata["identity_claims"].(*AgentIdentityClaims)
	require.NotNil(t, claims.SpiffeId)
	assert.Equal(t, "acme.com", claims.SpiffeId.TrustDomain)
}

// ── Regression: ensure empty scopes is [] not nil ────────────────────────

func TestExtractClaims_EmptyScopes(t *testing.T) {
	claims := ExtractClaims(map[string]interface{}{
		"iss": "issuer",
		"sub": "agent",
	})
	assert.NotNil(t, claims.Scopes)
	assert.Equal(t, []string{}, claims.Scopes)
}

// ── Helper: verify toString/toInt64 edge cases ──────────────────────────

func TestToString(t *testing.T) {
	assert.Equal(t, "", toString(nil))
	assert.Equal(t, "hello", toString("hello"))
	assert.Equal(t, "42", toString(42))
}

func TestToInt64(t *testing.T) {
	assert.Equal(t, int64(0), toInt64(nil))
	assert.Equal(t, int64(42), toInt64(float64(42)))
	assert.Equal(t, int64(42), toInt64(int64(42)))
	assert.Equal(t, int64(42), toInt64(42))
}

func TestToStringSlice(t *testing.T) {
	assert.Nil(t, toStringSlice(nil))
	assert.Equal(t, []string{"hello"}, toStringSlice("hello"))
	assert.Equal(t, []string{"a", "b"}, toStringSlice([]interface{}{"a", "b"}))
	assert.Equal(t, []string{"a", "b"}, toStringSlice([]string{"a", "b"}))
}

// Ensure unused import is exercised
var _ = strings.Join

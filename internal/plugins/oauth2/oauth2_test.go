package oauth2

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestPlugin(t *testing.T) *Plugin {
	t.Helper()
	p := New()
	err := p.Init(map[string]interface{}{
		"issuer":    "https://auth.example.com",
		"client_id": "test-client",
		"scopes": map[string]interface{}{
			"read":  "Read access",
			"write": "Write access",
		},
		"token_ttl":         3600,
		"refresh_token_ttl": 86400,
		"code_ttl":          600,
	})
	require.NoError(t, err)
	return p
}

func TestDiscoveryEndpoint(t *testing.T) {
	p := newTestPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	req.Host = "localhost:8080"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

	var meta map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &meta))

	assert.Equal(t, "https://auth.example.com", meta["issuer"])
	assert.Contains(t, meta["authorization_endpoint"], "/authorize")
	assert.Contains(t, meta["token_endpoint"], "/token")
	assert.Equal(t, []interface{}{"code"}, meta["response_types_supported"])
	assert.Equal(t, []interface{}{"S256"}, meta["code_challenge_methods_supported"])
	assert.Contains(t, meta, "scopes_supported")
}

func TestDiscoveryMethodNotAllowed(t *testing.T) {
	p := newTestPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	req := httptest.NewRequest(http.MethodPost, "/.well-known/oauth-authorization-server", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestAuthorizeEndpoint(t *testing.T) {
	p := newTestPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	codeChallenge := computeS256Challenge("test-verifier-1234567890abcdef")

	req := httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{
		"response_type":         {"code"},
		"client_id":             {"test-client"},
		"redirect_uri":         {"https://app.example.com/callback"},
		"state":                {"random-state-123"},
		"code_challenge":       {codeChallenge},
		"code_challenge_method": {"S256"},
		"scope":                {"read write"},
	}.Encode(), nil)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusFound, w.Code)
	loc := w.Header().Get("Location")
	assert.Contains(t, loc, "https://app.example.com/callback")
	assert.Contains(t, loc, "code=")
	assert.Contains(t, loc, "state=random-state-123")
}

func TestAuthorizeMissingParams(t *testing.T) {
	p := newTestPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	tests := []struct {
		name   string
		params url.Values
		errStr string
	}{
		{
			name:   "missing response_type",
			params: url.Values{"client_id": {"test-client"}},
			errStr: "unsupported_response_type",
		},
		{
			name:   "missing client_id",
			params: url.Values{"response_type": {"code"}},
			errStr: "invalid_request",
		},
		{
			name: "wrong client_id",
			params: url.Values{
				"response_type": {"code"},
				"client_id":     {"wrong-client"},
				"redirect_uri":  {"https://example.com/cb"},
			},
			errStr: "invalid_client",
		},
		{
			name: "missing redirect_uri",
			params: url.Values{
				"response_type": {"code"},
				"client_id":     {"test-client"},
			},
			errStr: "invalid_request",
		},
		{
			name: "missing code_challenge",
			params: url.Values{
				"response_type": {"code"},
				"client_id":     {"test-client"},
				"redirect_uri":  {"https://example.com/cb"},
			},
			errStr: "invalid_request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/authorize?"+tt.params.Encode(), nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			var errResp OAuth2Error
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))
			assert.Equal(t, tt.errStr, errResp.Error)
		})
	}
}

func TestPKCEFlowEndToEnd(t *testing.T) {
	p := newTestPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	codeVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	codeChallenge := computeS256Challenge(codeVerifier)

	// Step 1: Authorize → get code.
	authReq := httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{
		"response_type":         {"code"},
		"client_id":             {"test-client"},
		"redirect_uri":         {"https://app.example.com/callback"},
		"state":                {"state123"},
		"code_challenge":       {codeChallenge},
		"code_challenge_method": {"S256"},
		"scope":                {"read"},
	}.Encode(), nil)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, authReq)
	require.Equal(t, http.StatusFound, w.Code)

	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	code := loc.Query().Get("code")
	require.NotEmpty(t, code)

	// Step 2: Exchange code for tokens.
	tokenReq := httptest.NewRequest(http.MethodPost, "/token",
		strings.NewReader(url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"client_id":     {"test-client"},
			"code_verifier": {codeVerifier},
			"redirect_uri":  {"https://app.example.com/callback"},
		}.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, tokenReq)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))

	var tokenResp TokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tokenResp))
	assert.NotEmpty(t, tokenResp.AccessToken)
	assert.Equal(t, "Bearer", tokenResp.TokenType)
	assert.Equal(t, 3600, tokenResp.ExpiresIn)
	assert.NotEmpty(t, tokenResp.RefreshToken)
	assert.Equal(t, "read", tokenResp.Scope)

	// Step 3: Validate token.
	valid, errMsg := p.ValidateToken(tokenResp.AccessToken, []string{"read"})
	assert.True(t, valid)
	assert.Empty(t, errMsg)

	// Token without required scope should fail.
	valid, errMsg = p.ValidateToken(tokenResp.AccessToken, []string{"admin"})
	assert.False(t, valid)
	assert.Contains(t, errMsg, "missing_scope")
}

func TestPKCEVerificationFailure(t *testing.T) {
	p := newTestPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	codeChallenge := computeS256Challenge("correct-verifier")

	// Authorize.
	authReq := httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{
		"response_type":   {"code"},
		"client_id":       {"test-client"},
		"redirect_uri":    {"https://example.com/cb"},
		"code_challenge":  {codeChallenge},
		"scope":           {"read"},
	}.Encode(), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, authReq)
	loc, _ := url.Parse(w.Header().Get("Location"))
	code := loc.Query().Get("code")

	// Exchange with wrong verifier.
	tokenReq := httptest.NewRequest(http.MethodPost, "/token",
		strings.NewReader(url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"client_id":     {"test-client"},
			"code_verifier": {"wrong-verifier"},
		}.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, tokenReq)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var errResp OAuth2Error
	json.Unmarshal(w.Body.Bytes(), &errResp)
	assert.Equal(t, "invalid_grant", errResp.Error)
	assert.Contains(t, errResp.ErrorDescription, "PKCE")
}

func TestTokenRefresh(t *testing.T) {
	p := newTestPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	// Get initial tokens through PKCE flow.
	codeVerifier := "test-verifier-for-refresh-flow"
	codeChallenge := computeS256Challenge(codeVerifier)

	authReq := httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{
		"response_type":  {"code"},
		"client_id":      {"test-client"},
		"redirect_uri":   {"https://example.com/cb"},
		"code_challenge": {codeChallenge},
		"scope":          {"read write"},
	}.Encode(), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, authReq)
	loc, _ := url.Parse(w.Header().Get("Location"))
	code := loc.Query().Get("code")

	tokenReq := httptest.NewRequest(http.MethodPost, "/token",
		strings.NewReader(url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"client_id":     {"test-client"},
			"code_verifier": {codeVerifier},
		}.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, tokenReq)

	var initialTokens TokenResponse
	json.Unmarshal(w.Body.Bytes(), &initialTokens)

	// Refresh tokens.
	refreshReq := httptest.NewRequest(http.MethodPost, "/token",
		strings.NewReader(url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {initialTokens.RefreshToken},
			"client_id":     {"test-client"},
		}.Encode()))
	refreshReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, refreshReq)

	require.Equal(t, http.StatusOK, w.Code)

	var newTokens TokenResponse
	json.Unmarshal(w.Body.Bytes(), &newTokens)
	assert.NotEmpty(t, newTokens.AccessToken)
	assert.NotEqual(t, initialTokens.AccessToken, newTokens.AccessToken)
	assert.NotEmpty(t, newTokens.RefreshToken)
	assert.Equal(t, "read write", newTokens.Scope)

	// Old access token should be invalid.
	valid, _ := p.ValidateToken(initialTokens.AccessToken, nil)
	assert.False(t, valid)

	// New access token should be valid.
	valid, _ = p.ValidateToken(newTokens.AccessToken, nil)
	assert.True(t, valid)
}

func TestExpiredCode(t *testing.T) {
	p := newTestPlugin(t)
	now := time.Now()
	p.NowFunc = func() time.Time { return now }
	handler := p.Middleware()(http.NotFoundHandler())

	codeVerifier := "verifier-for-expiry-test"
	codeChallenge := computeS256Challenge(codeVerifier)

	// Authorize.
	authReq := httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{
		"response_type":  {"code"},
		"client_id":      {"test-client"},
		"redirect_uri":   {"https://example.com/cb"},
		"code_challenge": {codeChallenge},
	}.Encode(), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, authReq)
	loc, _ := url.Parse(w.Header().Get("Location"))
	code := loc.Query().Get("code")

	// Fast-forward past code TTL.
	p.NowFunc = func() time.Time { return now.Add(11 * time.Minute) }

	tokenReq := httptest.NewRequest(http.MethodPost, "/token",
		strings.NewReader(url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"client_id":     {"test-client"},
			"code_verifier": {codeVerifier},
		}.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, tokenReq)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var errResp OAuth2Error
	json.Unmarshal(w.Body.Bytes(), &errResp)
	assert.Equal(t, "invalid_grant", errResp.Error)
}

func TestExpiredToken(t *testing.T) {
	p := newTestPlugin(t)
	now := time.Now()
	p.NowFunc = func() time.Time { return now }

	// Manually insert a token.
	p.tokens.Store("expired-token", &tokenRecord{
		AccessToken: "expired-token",
		ClientID:    "test-client",
		Scopes:      []string{"read"},
		ExpiresAt:   now.Add(-1 * time.Hour), // already expired
	})

	valid, errMsg := p.ValidateToken("expired-token", nil)
	assert.False(t, valid)
	assert.Equal(t, "token_expired", errMsg)
}

func TestPassthroughNonOAuthPaths(t *testing.T) {
	p := newTestPlugin(t)
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := p.Middleware()(next)

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestTokenEndpointAuth(t *testing.T) {
	p := New()
	err := p.Init(map[string]interface{}{
		"client_id":     "test-client",
		"client_secret": "super-secret",
		"token_ttl":     3600,
		"code_ttl":      600,
	})
	require.NoError(t, err)
	handler := p.Middleware()(http.NotFoundHandler())

	codeVerifier := "verifier-for-secret-test"
	codeChallenge := computeS256Challenge(codeVerifier)

	// Authorize.
	authReq := httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{
		"response_type":  {"code"},
		"client_id":      {"test-client"},
		"redirect_uri":   {"https://example.com/cb"},
		"code_challenge": {codeChallenge},
	}.Encode(), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, authReq)
	loc, _ := url.Parse(w.Header().Get("Location"))
	code := loc.Query().Get("code")

	// Exchange without secret → fail.
	tokenReq := httptest.NewRequest(http.MethodPost, "/token",
		strings.NewReader(url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"client_id":     {"test-client"},
			"code_verifier": {codeVerifier},
		}.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, tokenReq)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// computeS256Challenge computes SHA256(verifier) as base64url (no padding).
func computeS256Challenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

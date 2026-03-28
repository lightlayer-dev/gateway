// Package oauth2 implements an OAuth2 authorization server with PKCE support.
// Ported from agent-layer-ts oauth2.ts and oauth2-handler.ts.
package oauth2

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lightlayer-dev/gateway/internal/plugins"
)

func init() {
	plugins.Register("oauth2", func() plugins.Plugin { return New() })
}

// ── Types ────────────────────────────────────────────────────────────────

// Config holds OAuth2 server configuration.
type Config struct {
	Issuer          string
	ClientID        string
	ClientSecret    string
	RedirectURI     string
	Scopes          map[string]string
	TokenTTL        int // seconds
	RefreshTokenTTL int // seconds
	CodeTTL         int // seconds
	Audience        string
}

// TokenResponse is the OAuth2 token endpoint response.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// OAuth2Error is the standard OAuth2 error response.
type OAuth2Error struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// authCode is an in-memory authorization code record.
type authCode struct {
	Code          string
	ClientID      string
	RedirectURI   string
	CodeChallenge string
	Scopes        []string
	ExpiresAt     time.Time
}

// tokenRecord stores issued token metadata.
type tokenRecord struct {
	AccessToken  string
	RefreshToken string
	ClientID     string
	Scopes       []string
	ExpiresAt    time.Time
	RefreshExp   time.Time
}

// ── Plugin ───────────────────────────────────────────────────────────────

// Plugin implements the OAuth2 authorization server.
type Plugin struct {
	cfg    Config
	codes  sync.Map // code string → *authCode
	tokens sync.Map // access_token → *tokenRecord
	refresh sync.Map // refresh_token → *tokenRecord

	// NowFunc allows overriding time.Now for testing.
	NowFunc func() time.Time
}

// New creates a new OAuth2 plugin.
func New() *Plugin {
	return &Plugin{NowFunc: time.Now}
}

func (p *Plugin) Name() string { return "oauth2" }

func (p *Plugin) Init(cfg map[string]interface{}) error {
	if v, ok := cfg["issuer"].(string); ok {
		p.cfg.Issuer = v
	}
	if v, ok := cfg["client_id"].(string); ok {
		p.cfg.ClientID = v
	}
	if v, ok := cfg["client_secret"].(string); ok {
		p.cfg.ClientSecret = v
	}
	if v, ok := cfg["redirect_uri"].(string); ok {
		p.cfg.RedirectURI = v
	}
	if v, ok := cfg["audience"].(string); ok {
		p.cfg.Audience = v
	}
	if v, ok := cfg["scopes"].(map[string]string); ok {
		p.cfg.Scopes = v
	} else if v, ok := cfg["scopes"].(map[string]interface{}); ok {
		p.cfg.Scopes = make(map[string]string, len(v))
		for k, val := range v {
			p.cfg.Scopes[k] = fmt.Sprintf("%v", val)
		}
	}

	p.cfg.TokenTTL = intFromCfg(cfg, "token_ttl", 3600)
	p.cfg.RefreshTokenTTL = intFromCfg(cfg, "refresh_token_ttl", 86400)
	p.cfg.CodeTTL = intFromCfg(cfg, "code_ttl", 600)

	// Start TTL cleanup goroutine.
	go p.cleanupLoop()

	slog.Info("oauth2: initialized",
		"issuer", p.cfg.Issuer,
		"scopes", len(p.cfg.Scopes),
	)
	return nil
}

func (p *Plugin) Close() error { return nil }

func (p *Plugin) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/.well-known/oauth-authorization-server":
				p.handleDiscovery(w, r)
				return
			case "/authorize":
				p.handleAuthorize(w, r)
				return
			case "/token":
				p.handleToken(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ── Discovery Endpoint (RFC 8414) ────────────────────────────────────────

func (p *Plugin) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeOAuth2Error(w, http.StatusMethodNotAllowed, "invalid_request", "Method not allowed")
		return
	}

	metadata := map[string]interface{}{
		"authorization_endpoint":            p.authorizationEndpoint(r),
		"token_endpoint":                    p.tokenEndpoint(r),
		"response_types_supported":          []string{"code"},
		"grant_types_supported":             []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":  []string{"S256"},
		"token_endpoint_auth_methods_supported": p.tokenEndpointAuthMethods(),
	}

	if p.cfg.Issuer != "" {
		metadata["issuer"] = p.cfg.Issuer
	}
	if len(p.cfg.Scopes) > 0 {
		scopes := make([]string, 0, len(p.cfg.Scopes))
		for k := range p.cfg.Scopes {
			scopes = append(scopes, k)
		}
		metadata["scopes_supported"] = scopes
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metadata)
}

// ── Authorize Endpoint ───────────────────────────────────────────────────

func (p *Plugin) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeOAuth2Error(w, http.StatusMethodNotAllowed, "invalid_request", "Method not allowed")
		return
	}

	q := r.URL.Query()
	if r.Method == http.MethodPost {
		r.ParseForm()
		for k, v := range r.PostForm {
			q[k] = v
		}
	}

	responseType := q.Get("response_type")
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	state := q.Get("state")
	codeChallenge := q.Get("code_challenge")
	codeChallengeMethod := q.Get("code_challenge_method")
	scope := q.Get("scope")

	// Validate required params.
	if responseType != "code" {
		writeOAuth2Error(w, http.StatusBadRequest, "unsupported_response_type", "Only 'code' response_type is supported")
		return
	}
	if clientID == "" {
		writeOAuth2Error(w, http.StatusBadRequest, "invalid_request", "client_id is required")
		return
	}
	if p.cfg.ClientID != "" && clientID != p.cfg.ClientID {
		writeOAuth2Error(w, http.StatusBadRequest, "invalid_client", "Unknown client_id")
		return
	}
	if redirectURI == "" {
		writeOAuth2Error(w, http.StatusBadRequest, "invalid_request", "redirect_uri is required")
		return
	}
	if p.cfg.RedirectURI != "" && redirectURI != p.cfg.RedirectURI {
		writeOAuth2Error(w, http.StatusBadRequest, "invalid_request", "redirect_uri mismatch")
		return
	}
	if codeChallenge == "" {
		writeOAuth2Error(w, http.StatusBadRequest, "invalid_request", "code_challenge is required for PKCE")
		return
	}
	if codeChallengeMethod != "" && codeChallengeMethod != "S256" {
		writeOAuth2Error(w, http.StatusBadRequest, "invalid_request", "Only S256 code_challenge_method is supported")
		return
	}

	// Parse scopes.
	var scopes []string
	if scope != "" {
		scopes = strings.Split(scope, " ")
	}

	// Generate authorization code.
	code := generateRandomHex(32)
	ac := &authCode{
		Code:          code,
		ClientID:      clientID,
		RedirectURI:   redirectURI,
		CodeChallenge: codeChallenge,
		Scopes:        scopes,
		ExpiresAt:     p.NowFunc().Add(time.Duration(p.cfg.CodeTTL) * time.Second),
	}
	p.codes.Store(code, ac)

	// Redirect with code and state.
	redirectURL := redirectURI + "?code=" + code
	if state != "" {
		redirectURL += "&state=" + state
	}

	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// ── Token Endpoint ───────────────────────────────────────────────────────

func (p *Plugin) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOAuth2Error(w, http.StatusMethodNotAllowed, "invalid_request", "Method not allowed")
		return
	}

	if err := r.ParseForm(); err != nil {
		writeOAuth2Error(w, http.StatusBadRequest, "invalid_request", "Invalid form body")
		return
	}

	grantType := r.PostFormValue("grant_type")

	switch grantType {
	case "authorization_code":
		p.handleAuthCodeExchange(w, r)
	case "refresh_token":
		p.handleRefreshToken(w, r)
	default:
		writeOAuth2Error(w, http.StatusBadRequest, "unsupported_grant_type",
			fmt.Sprintf("Unsupported grant_type: %s", grantType))
	}
}

func (p *Plugin) handleAuthCodeExchange(w http.ResponseWriter, r *http.Request) {
	code := r.PostFormValue("code")
	clientID := r.PostFormValue("client_id")
	codeVerifier := r.PostFormValue("code_verifier")
	redirectURI := r.PostFormValue("redirect_uri")

	if code == "" {
		writeOAuth2Error(w, http.StatusBadRequest, "invalid_request", "code is required")
		return
	}
	if codeVerifier == "" {
		writeOAuth2Error(w, http.StatusBadRequest, "invalid_request", "code_verifier is required for PKCE")
		return
	}

	// Look up and consume the auth code.
	val, ok := p.codes.LoadAndDelete(code)
	if !ok {
		writeOAuth2Error(w, http.StatusBadRequest, "invalid_grant", "Invalid or expired authorization code")
		return
	}
	ac := val.(*authCode)

	// Validate expiry.
	if p.NowFunc().After(ac.ExpiresAt) {
		writeOAuth2Error(w, http.StatusBadRequest, "invalid_grant", "Authorization code expired")
		return
	}

	// Validate client_id.
	if clientID != "" && clientID != ac.ClientID {
		writeOAuth2Error(w, http.StatusBadRequest, "invalid_client", "client_id mismatch")
		return
	}

	// Validate redirect_uri.
	if redirectURI != "" && redirectURI != ac.RedirectURI {
		writeOAuth2Error(w, http.StatusBadRequest, "invalid_grant", "redirect_uri mismatch")
		return
	}

	// Verify PKCE: SHA-256(code_verifier) must equal code_challenge.
	if !verifyPKCE(codeVerifier, ac.CodeChallenge) {
		writeOAuth2Error(w, http.StatusBadRequest, "invalid_grant", "PKCE code_verifier verification failed")
		return
	}

	// Validate client secret if configured.
	if p.cfg.ClientSecret != "" {
		clientSecret := r.PostFormValue("client_secret")
		if clientSecret != p.cfg.ClientSecret {
			writeOAuth2Error(w, http.StatusUnauthorized, "invalid_client", "Invalid client credentials")
			return
		}
	}

	// Issue tokens.
	p.issueTokens(w, ac.ClientID, ac.Scopes)
}

func (p *Plugin) handleRefreshToken(w http.ResponseWriter, r *http.Request) {
	refreshToken := r.PostFormValue("refresh_token")
	clientID := r.PostFormValue("client_id")

	if refreshToken == "" {
		writeOAuth2Error(w, http.StatusBadRequest, "invalid_request", "refresh_token is required")
		return
	}

	// Look up refresh token.
	val, ok := p.refresh.LoadAndDelete(refreshToken)
	if !ok {
		writeOAuth2Error(w, http.StatusBadRequest, "invalid_grant", "Invalid or expired refresh token")
		return
	}
	rec := val.(*tokenRecord)

	// Validate expiry.
	if p.NowFunc().After(rec.RefreshExp) {
		writeOAuth2Error(w, http.StatusBadRequest, "invalid_grant", "Refresh token expired")
		return
	}

	// Validate client_id.
	if clientID != "" && clientID != rec.ClientID {
		writeOAuth2Error(w, http.StatusBadRequest, "invalid_client", "client_id mismatch")
		return
	}

	// Validate client secret if configured.
	if p.cfg.ClientSecret != "" {
		clientSecret := r.PostFormValue("client_secret")
		if clientSecret != p.cfg.ClientSecret {
			writeOAuth2Error(w, http.StatusUnauthorized, "invalid_client", "Invalid client credentials")
			return
		}
	}

	// Revoke old access token.
	p.tokens.Delete(rec.AccessToken)

	// Issue new tokens.
	p.issueTokens(w, rec.ClientID, rec.Scopes)
}

func (p *Plugin) issueTokens(w http.ResponseWriter, clientID string, scopes []string) {
	now := p.NowFunc()
	accessToken := generateRandomHex(32)
	refreshToken := generateRandomHex(32)

	rec := &tokenRecord{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ClientID:     clientID,
		Scopes:       scopes,
		ExpiresAt:    now.Add(time.Duration(p.cfg.TokenTTL) * time.Second),
		RefreshExp:   now.Add(time.Duration(p.cfg.RefreshTokenTTL) * time.Second),
	}

	p.tokens.Store(accessToken, rec)
	p.refresh.Store(refreshToken, rec)

	resp := TokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    p.cfg.TokenTTL,
		RefreshToken: refreshToken,
	}
	if len(scopes) > 0 {
		resp.Scope = strings.Join(scopes, " ")
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	json.NewEncoder(w).Encode(resp)
}

// ValidateToken checks if an access token is valid and has the required scopes.
// Exported for use by other plugins (e.g., MCP, API proxy).
func (p *Plugin) ValidateToken(token string, requiredScopes []string) (bool, string) {
	val, ok := p.tokens.Load(token)
	if !ok {
		return false, "invalid_token"
	}
	rec := val.(*tokenRecord)

	if p.NowFunc().After(rec.ExpiresAt) {
		p.tokens.Delete(token)
		return false, "token_expired"
	}

	if len(requiredScopes) > 0 {
		scopeSet := make(map[string]bool, len(rec.Scopes))
		for _, s := range rec.Scopes {
			scopeSet[s] = true
		}
		for _, s := range requiredScopes {
			if !scopeSet[s] {
				return false, fmt.Sprintf("missing_scope: %s", s)
			}
		}
	}

	return true, ""
}

// ── Helpers ──────────────────────────────────────────────────────────────

func (p *Plugin) authorizationEndpoint(r *http.Request) string {
	return schemeHost(r) + "/authorize"
}

func (p *Plugin) tokenEndpoint(r *http.Request) string {
	return schemeHost(r) + "/token"
}

func (p *Plugin) tokenEndpointAuthMethods() []string {
	if p.cfg.ClientSecret != "" {
		return []string{"client_secret_post"}
	}
	return []string{"none"}
}

func schemeHost(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if fwd := r.Header.Get("X-Forwarded-Proto"); fwd != "" {
		scheme = fwd
	}
	return scheme + "://" + r.Host
}

// verifyPKCE checks that SHA256(codeVerifier) == codeChallenge (base64url-encoded).
func verifyPKCE(codeVerifier, codeChallenge string) bool {
	h := sha256.Sum256([]byte(codeVerifier))
	computed := base64.RawURLEncoding.EncodeToString(h[:])
	return computed == codeChallenge
}

func generateRandomHex(byteLen int) string {
	b := make([]byte, byteLen)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func writeOAuth2Error(w http.ResponseWriter, status int, errCode, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(OAuth2Error{
		Error:            errCode,
		ErrorDescription: desc,
	})
}

func intFromCfg(cfg map[string]interface{}, key string, def int) int {
	if v, ok := cfg[key]; ok {
		switch val := v.(type) {
		case int:
			return val
		case float64:
			return int(val)
		case int64:
			return int(val)
		}
	}
	return def
}

// cleanupLoop periodically removes expired codes and tokens.
func (p *Plugin) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := p.NowFunc()
		p.codes.Range(func(key, val interface{}) bool {
			if ac := val.(*authCode); now.After(ac.ExpiresAt) {
				p.codes.Delete(key)
			}
			return true
		})
		p.tokens.Range(func(key, val interface{}) bool {
			if rec := val.(*tokenRecord); now.After(rec.ExpiresAt) {
				p.tokens.Delete(key)
			}
			return true
		})
		p.refresh.Range(func(key, val interface{}) bool {
			if rec := val.(*tokenRecord); now.After(rec.RefreshExp) {
				p.refresh.Delete(key)
			}
			return true
		})
	}
}

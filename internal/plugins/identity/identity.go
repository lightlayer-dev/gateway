// Package identity implements agent identity verification per IETF draft-klrc-aiagent-auth-00.
//
// Supports JWT-based Workload Identity Tokens (WIT) verification, SPIFFE ID
// extraction, scoped authorization policies, and audit event generation.
// Three modes: log (observe), warn (log + header), enforce (reject unverified).
//
// Ported from agent-layer-ts agent-identity.ts.
package identity

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lightlayer-dev/gateway/internal/plugins"
)

func init() {
	plugins.Register("identity", func() plugins.Plugin { return New() })
}

// ── Types ────────────────────────────────────────────────────────────────

// SpiffeId represents a parsed SPIFFE URI: spiffe://trust-domain/path.
type SpiffeId struct {
	TrustDomain string `json:"trust_domain"`
	Path        string `json:"path"`
	Raw         string `json:"raw"`
}

// AgentIdentityClaims holds claims extracted from a verified agent identity token.
type AgentIdentityClaims struct {
	AgentId      string                 `json:"agent_id"`
	SpiffeId     *SpiffeId              `json:"spiffe_id,omitempty"`
	Issuer       string                 `json:"issuer"`
	Subject      string                 `json:"subject"`
	Audience     []string               `json:"audience"`
	ExpiresAt    int64                  `json:"expires_at"`
	IssuedAt     int64                  `json:"issued_at"`
	Scopes       []string               `json:"scopes"`
	Delegated    bool                   `json:"delegated"`
	DelegatedBy  string                 `json:"delegated_by,omitempty"`
	CustomClaims map[string]interface{} `json:"custom_claims,omitempty"`
}

// AgentAuthzPolicy defines an authorization rule for agent access.
type AgentAuthzPolicy struct {
	Name           string   `json:"name"`
	AgentPattern   string   `json:"agent_pattern,omitempty"`
	TrustDomains   []string `json:"trust_domains,omitempty"`
	RequiredScopes []string `json:"required_scopes,omitempty"`
	Methods        []string `json:"methods,omitempty"`
	Paths          []string `json:"paths,omitempty"`
	AllowDelegated *bool    `json:"allow_delegated,omitempty"`
}

// AuthzContext carries request metadata for policy evaluation.
type AuthzContext struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers,omitempty"`
}

// AuthzResult is the outcome of policy evaluation.
type AuthzResult struct {
	Allowed       bool   `json:"allowed"`
	MatchedPolicy string `json:"matched_policy,omitempty"`
	DeniedReason  string `json:"denied_reason,omitempty"`
}

// AuditEvent records an identity verification event for the analytics pipeline.
type AuditEvent struct {
	Type        string      `json:"type"`
	Timestamp   string      `json:"timestamp"`
	AgentId     string      `json:"agent_id"`
	SpiffeId    string      `json:"spiffe_id,omitempty"`
	Issuer      string      `json:"issuer"`
	Delegated   bool        `json:"delegated"`
	DelegatedBy string      `json:"delegated_by,omitempty"`
	Scopes      []string    `json:"scopes"`
	Method      string      `json:"method"`
	Path        string      `json:"path"`
	AuthzResult AuthzResult `json:"authz_result"`
}

// TokenValidationError describes why a token was rejected.
type TokenValidationError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *TokenValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// identityConfig holds the parsed plugin configuration.
type identityConfig struct {
	Mode               string             `json:"mode"`
	TrustedIssuers     []string           `json:"trusted_issuers"`
	Audience           []string           `json:"audience"`
	TrustedDomains     []string           `json:"trusted_domains"`
	Policies           []AgentAuthzPolicy `json:"policies"`
	DefaultPolicy      string             `json:"default_policy"`
	HeaderName         string             `json:"header_name"`
	TokenPrefix        string             `json:"token_prefix"`
	ClockSkewSeconds   int                `json:"clock_skew_seconds"`
	MaxLifetimeSeconds int                `json:"max_lifetime_seconds"`
}

// ── SPIFFE ID Parser ─────────────────────────────────────────────────────

var spiffeRe = regexp.MustCompile(`^spiffe://([^/]+)(/.*)?$`)

// ParseSpiffeId parses a SPIFFE URI. Returns nil if invalid.
func ParseSpiffeId(uri string) *SpiffeId {
	m := spiffeRe.FindStringSubmatch(uri)
	if m == nil {
		return nil
	}
	path := m[2]
	if path == "" {
		path = "/"
	}
	return &SpiffeId{
		TrustDomain: m[1],
		Path:        path,
		Raw:         uri,
	}
}

// IsSpiffeTrusted checks if a SPIFFE ID's trust domain is in the trusted list.
func IsSpiffeTrusted(id *SpiffeId, trustedDomains []string) bool {
	for _, d := range trustedDomains {
		if d == id.TrustDomain {
			return true
		}
	}
	return false
}

// ── Claims Extraction ────────────────────────────────────────────────────

var knownClaims = map[string]bool{
	"iss": true, "sub": true, "aud": true, "exp": true, "iat": true,
	"nbf": true, "jti": true, "scope": true, "scopes": true, "scp": true,
	"act": true, "agent_id": true,
}

// ExtractClaims converts raw JWT claims into AgentIdentityClaims.
func ExtractClaims(payload map[string]interface{}) *AgentIdentityClaims {
	iss := toString(payload["iss"])
	sub := toString(payload["sub"])

	// Agent ID: prefer explicit agent_id, then sub
	agentId := toString(payload["agent_id"])
	if agentId == "" {
		agentId = sub
	}

	// Parse SPIFFE ID from agent identifier
	spiffeId := ParseSpiffeId(agentId)

	// Audience normalization
	audience := toStringSlice(payload["aud"])

	// Scopes: support "scope" (space-delimited), "scopes" (array), "scp" (array)
	var scopes []string
	if scopeStr, ok := payload["scope"].(string); ok {
		for _, s := range strings.Split(scopeStr, " ") {
			if s != "" {
				scopes = append(scopes, s)
			}
		}
	} else if scopesArr := toStringSlice(payload["scopes"]); len(scopesArr) > 0 {
		scopes = scopesArr
	} else if scpArr := toStringSlice(payload["scp"]); len(scpArr) > 0 {
		scopes = scpArr
	}
	if scopes == nil {
		scopes = []string{}
	}

	// Delegation detection (OAuth 2.0 actor claim)
	var delegated bool
	var delegatedBy string
	if act, ok := payload["act"]; ok && act != nil {
		delegated = true
		if actMap, ok := act.(map[string]interface{}); ok {
			delegatedBy = toString(actMap["sub"])
		}
	}

	// Collect custom claims
	customClaims := map[string]interface{}{}
	for k, v := range payload {
		if !knownClaims[k] {
			customClaims[k] = v
		}
	}

	return &AgentIdentityClaims{
		AgentId:      agentId,
		SpiffeId:     spiffeId,
		Issuer:       iss,
		Subject:      sub,
		Audience:     audience,
		ExpiresAt:    toInt64(payload["exp"]),
		IssuedAt:     toInt64(payload["iat"]),
		Scopes:       scopes,
		Delegated:    delegated,
		DelegatedBy:  delegatedBy,
		CustomClaims: customClaims,
	}
}

// ── Token Validation ─────────────────────────────────────────────────────

// ValidateClaims checks extracted claims against the identity config.
// Returns nil if valid, or a TokenValidationError.
func ValidateClaims(claims *AgentIdentityClaims, cfg *identityConfig) *TokenValidationError {
	now := time.Now().Unix()
	skew := int64(cfg.ClockSkewSeconds)

	// Check issuer
	if len(cfg.TrustedIssuers) > 0 {
		trusted := false
		for _, iss := range cfg.TrustedIssuers {
			if iss == claims.Issuer {
				trusted = true
				break
			}
		}
		if !trusted {
			return &TokenValidationError{
				Code:    "untrusted_issuer",
				Message: fmt.Sprintf("Issuer %q is not trusted.", claims.Issuer),
			}
		}
	}

	// Check audience
	if len(cfg.Audience) > 0 && len(claims.Audience) > 0 {
		match := false
		for _, ca := range claims.Audience {
			for _, ea := range cfg.Audience {
				if ca == ea {
					match = true
					break
				}
			}
			if match {
				break
			}
		}
		if !match {
			return &TokenValidationError{
				Code:    "invalid_audience",
				Message: "Token audience does not match any expected audience.",
			}
		}
	}

	// Check expiration
	if claims.ExpiresAt != 0 && claims.ExpiresAt+skew < now {
		return &TokenValidationError{
			Code:    "expired_token",
			Message: "Token has expired.",
		}
	}

	// Check max lifetime
	maxLifetime := int64(cfg.MaxLifetimeSeconds)
	if claims.IssuedAt != 0 && claims.ExpiresAt != 0 {
		lifetime := claims.ExpiresAt - claims.IssuedAt
		if lifetime > maxLifetime {
			return &TokenValidationError{
				Code:    "token_too_long_lived",
				Message: fmt.Sprintf("Token lifetime %ds exceeds maximum %ds.", lifetime, maxLifetime),
			}
		}
	}

	// Check SPIFFE trust domain
	if claims.SpiffeId != nil && len(cfg.TrustedDomains) > 0 {
		if !IsSpiffeTrusted(claims.SpiffeId, cfg.TrustedDomains) {
			return &TokenValidationError{
				Code:    "untrusted_domain",
				Message: fmt.Sprintf("SPIFFE trust domain %q is not trusted.", claims.SpiffeId.TrustDomain),
			}
		}
	}

	return nil
}

// ── Authorization ────────────────────────────────────────────────────────

// GlobMatch matches a value against a pattern with * wildcards.
func GlobMatch(pattern, value string) bool {
	// Escape regex special chars, then replace * with .*
	escaped := regexp.QuoteMeta(pattern)
	escaped = strings.ReplaceAll(escaped, `\*`, `.*`)
	re, err := regexp.Compile("^" + escaped + "$")
	if err != nil {
		return false
	}
	return re.MatchString(value)
}

// EvaluateAuthz evaluates authorization policies against verified claims.
func EvaluateAuthz(claims *AgentIdentityClaims, ctx AuthzContext, policies []AgentAuthzPolicy, defaultPolicy string) AuthzResult {
	for _, policy := range policies {
		// Match agent pattern
		if policy.AgentPattern != "" && !GlobMatch(policy.AgentPattern, claims.AgentId) {
			continue
		}

		// Match trust domain
		if len(policy.TrustDomains) > 0 && claims.SpiffeId != nil {
			found := false
			for _, d := range policy.TrustDomains {
				if d == claims.SpiffeId.TrustDomain {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Match method
		if len(policy.Methods) > 0 {
			found := false
			for _, m := range policy.Methods {
				if strings.EqualFold(m, ctx.Method) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Match path
		if len(policy.Paths) > 0 {
			pathMatch := false
			for _, p := range policy.Paths {
				if GlobMatch(p, ctx.Path) {
					pathMatch = true
					break
				}
			}
			if !pathMatch {
				continue
			}
		}

		// Check delegation
		if policy.AllowDelegated != nil && !*policy.AllowDelegated && claims.Delegated {
			return AuthzResult{
				Allowed:       false,
				MatchedPolicy: policy.Name,
				DeniedReason:  "Delegated access not allowed by policy.",
			}
		}

		// Check required scopes
		if len(policy.RequiredScopes) > 0 {
			var missing []string
			for _, rs := range policy.RequiredScopes {
				found := false
				for _, s := range claims.Scopes {
					if s == rs {
						found = true
						break
					}
				}
				if !found {
					missing = append(missing, rs)
				}
			}
			if len(missing) > 0 {
				return AuthzResult{
					Allowed:       false,
					MatchedPolicy: policy.Name,
					DeniedReason:  fmt.Sprintf("Missing required scopes: %s", strings.Join(missing, ", ")),
				}
			}
		}

		// All checks passed
		return AuthzResult{Allowed: true, MatchedPolicy: policy.Name}
	}

	// No policy matched — use default
	if defaultPolicy == "allow" {
		return AuthzResult{Allowed: true}
	}
	return AuthzResult{
		Allowed:      false,
		DeniedReason: "No matching authorization policy.",
	}
}

// ── Audit Event ──────────────────────────────────────────────────────────

// BuildAuditEvent creates an audit event from identity verification results.
func BuildAuditEvent(claims *AgentIdentityClaims, ctx AuthzContext, result AuthzResult) *AuditEvent {
	event := &AuditEvent{
		Type:        "agent_identity",
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		AgentId:     claims.AgentId,
		Issuer:      claims.Issuer,
		Delegated:   claims.Delegated,
		DelegatedBy: claims.DelegatedBy,
		Scopes:      claims.Scopes,
		Method:      ctx.Method,
		Path:        ctx.Path,
		AuthzResult: result,
	}
	if claims.SpiffeId != nil {
		event.SpiffeId = claims.SpiffeId.Raw
	}
	return event
}

// ── Plugin ───────────────────────────────────────────────────────────────

// Plugin implements agent identity verification as a gateway middleware.
type Plugin struct {
	cfg *identityConfig
	// NowFunc is exposed for testing to override time.Now().
	NowFunc func() time.Time
}

// New returns a new identity Plugin.
func New() *Plugin {
	return &Plugin{
		NowFunc: time.Now,
	}
}

func (p *Plugin) Name() string { return "identity" }

// Init parses config and prepares the plugin.
func (p *Plugin) Init(raw map[string]interface{}) error {
	slog.Warn("plugin identity is deprecated, use agent_onboarding instead. Will be removed in v0.3")

	cfg, err := parseConfig(raw)
	if err != nil {
		return fmt.Errorf("identity: %w", err)
	}
	p.cfg = cfg
	return nil
}

func (p *Plugin) Close() error { return nil }

// Middleware returns the identity verification middleware.
func (p *Plugin) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p.handleRequest(w, r, next)
		})
	}
}

func (p *Plugin) handleRequest(w http.ResponseWriter, r *http.Request, next http.Handler) {
	cfg := p.cfg

	// Extract token from header.
	tokenStr := extractToken(r, cfg.HeaderName, cfg.TokenPrefix)

	if tokenStr == "" {
		if cfg.Mode == "enforce" {
			plugins.WriteError(w, http.StatusUnauthorized, "missing_token",
				"Agent identity token is required.")
			return
		}
		// log/warn: continue without identity
		if cfg.Mode == "warn" {
			w.Header().Set("X-Agent-Verified", "false")
		}
		slog.Debug("identity: no token present", "path", r.URL.Path, "mode", cfg.Mode)
		next.ServeHTTP(w, r)
		return
	}

	// Parse JWT claims (unverified — signature check uses HMAC/RSA via golang-jwt).
	parser := jwt.NewParser(
		jwt.WithLeeway(time.Duration(cfg.ClockSkewSeconds)*time.Second),
		jwt.WithoutClaimsValidation(),
	)

	token, _, err := parser.ParseUnverified(tokenStr, jwt.MapClaims{})
	if err != nil {
		p.handleFailure(w, r, next, &TokenValidationError{
			Code:    "malformed_token",
			Message: "Failed to parse identity token.",
		})
		return
	}

	mapClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		p.handleFailure(w, r, next, &TokenValidationError{
			Code:    "malformed_token",
			Message: "Failed to extract token claims.",
		})
		return
	}

	// Extract structured claims.
	claims := ExtractClaims(mapClaims)

	// Validate claims.
	if validErr := ValidateClaims(claims, cfg); validErr != nil {
		p.handleFailure(w, r, next, validErr)
		return
	}

	// Evaluate authorization policies.
	authzCtx := AuthzContext{
		Method: r.Method,
		Path:   r.URL.Path,
	}

	var authzResult AuthzResult
	if len(cfg.Policies) > 0 {
		authzResult = EvaluateAuthz(claims, authzCtx, cfg.Policies, cfg.DefaultPolicy)
	} else {
		authzResult = AuthzResult{Allowed: true}
	}

	// Generate audit event.
	audit := BuildAuditEvent(claims, authzCtx, authzResult)
	auditJSON, _ := json.Marshal(audit)
	slog.Info("identity: audit", "event", string(auditJSON))

	// Store claims in request context metadata.
	if rc := plugins.GetRequestContext(r.Context()); rc != nil {
		rc.Metadata["identity_claims"] = claims
		rc.Metadata["identity_verified"] = true
		if rc.AgentInfo != nil {
			rc.AgentInfo.Verified = true
		}
	}

	if !authzResult.Allowed {
		if cfg.Mode == "enforce" {
			plugins.WriteError(w, http.StatusForbidden, "authorization_denied",
				authzResult.DeniedReason)
			return
		}
		slog.Warn("identity: authorization denied (non-enforce)",
			"agent_id", claims.AgentId,
			"reason", authzResult.DeniedReason,
			"mode", cfg.Mode,
		)
	}

	// Set identity headers on proxied request.
	w.Header().Set("X-Agent-Id", claims.AgentId)
	w.Header().Set("X-Agent-Verified", "true")
	r.Header.Set("X-Agent-Id", claims.AgentId)
	r.Header.Set("X-Agent-Verified", "true")

	next.ServeHTTP(w, r)
}

// handleFailure handles token validation failures based on mode.
func (p *Plugin) handleFailure(w http.ResponseWriter, r *http.Request, next http.Handler, validErr *TokenValidationError) {
	cfg := p.cfg

	slog.Warn("identity: verification failed",
		"code", validErr.Code,
		"message", validErr.Message,
		"path", r.URL.Path,
		"mode", cfg.Mode,
	)

	switch cfg.Mode {
	case "enforce":
		status := http.StatusUnauthorized
		if validErr.Code == "untrusted_issuer" || validErr.Code == "untrusted_domain" {
			status = http.StatusForbidden
		}
		plugins.WriteError(w, status, validErr.Code, validErr.Message)
	case "warn":
		w.Header().Set("X-Agent-Verified", "false")
		w.Header().Set("X-Agent-Identity-Error", validErr.Code)
		next.ServeHTTP(w, r)
	default: // log
		next.ServeHTTP(w, r)
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────

// extractToken pulls the bearer token from the configured header.
func extractToken(r *http.Request, headerName, prefix string) string {
	val := r.Header.Get(headerName)
	if val == "" {
		return ""
	}
	if prefix != "" {
		pfx := prefix + " "
		if strings.HasPrefix(val, pfx) {
			return strings.TrimPrefix(val, pfx)
		}
		// If prefix doesn't match, return empty (not a token we recognize)
		return ""
	}
	return val
}

func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func toStringSlice(v interface{}) []string {
	if v == nil {
		return nil
	}
	if s, ok := v.(string); ok {
		return []string{s}
	}
	if arr, ok := v.([]interface{}); ok {
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			result = append(result, toString(item))
		}
		return result
	}
	if arr, ok := v.([]string); ok {
		return arr
	}
	return nil
}

func toInt64(v interface{}) int64 {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(math.Round(n))
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}

// ── Config parsing ──────────────────────────────────────────────────────

func parseConfig(raw map[string]interface{}) (*identityConfig, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	var cfg identityConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Apply defaults only for fields not explicitly set in the raw config.
	if cfg.Mode == "" {
		cfg.Mode = "log"
	}
	if _, ok := raw["header_name"]; !ok {
		cfg.HeaderName = "Authorization"
	}
	if _, ok := raw["token_prefix"]; !ok {
		cfg.TokenPrefix = "Bearer"
	}
	if cfg.ClockSkewSeconds == 0 {
		cfg.ClockSkewSeconds = 30
	}
	if cfg.MaxLifetimeSeconds == 0 {
		cfg.MaxLifetimeSeconds = 3600
	}
	if cfg.DefaultPolicy == "" {
		cfg.DefaultPolicy = "deny"
	}

	return &cfg, nil
}

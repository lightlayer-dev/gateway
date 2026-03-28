package onboarding

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lightlayer-dev/gateway/internal/plugins"
)

func init() {
	plugins.Register("agent_onboarding", func() plugins.Plugin { return New() })
}

// NowFunc is overridable for testing.
var NowFunc = time.Now

// Plugin implements agent onboarding: registration endpoint + 401 for unauthed agents.
type Plugin struct {
	cfg     Config
	webhook *WebhookClient

	// Rate limiting: per-IP sliding window.
	mu      sync.Mutex
	windows map[string]*rateLimitWindow
}

type rateLimitWindow struct {
	count   int
	resetAt time.Time
}

// New returns a new onboarding Plugin.
func New() *Plugin {
	return &Plugin{
		windows: make(map[string]*rateLimitWindow),
	}
}

func (p *Plugin) Name() string { return "agent_onboarding" }

func (p *Plugin) Init(cfg map[string]interface{}) error {
	if v, ok := cfg["provisioning_webhook"].(string); ok {
		p.cfg.ProvisioningWebhook = v
	}
	if p.cfg.ProvisioningWebhook == "" {
		return fmt.Errorf("agent_onboarding: provisioning_webhook is required")
	}

	if v, ok := cfg["webhook_secret"].(string); ok {
		p.cfg.WebhookSecret = v
	}

	p.cfg.WebhookTimeout = 10 * time.Second
	if v, ok := cfg["webhook_timeout"].(string); ok {
		if d, err := time.ParseDuration(v); err == nil {
			p.cfg.WebhookTimeout = d
		}
	}

	if v, ok := cfg["require_identity"].(bool); ok {
		p.cfg.RequireIdentity = v
	}

	if v, ok := cfg["auth_docs"].(string); ok {
		p.cfg.AuthDocs = v
	}

	if v, ok := cfg["allowed_providers"]; ok {
		switch vv := v.(type) {
		case []interface{}:
			for _, item := range vv {
				if s, ok := item.(string); ok {
					p.cfg.AllowedProviders = append(p.cfg.AllowedProviders, s)
				}
			}
		case []string:
			p.cfg.AllowedProviders = vv
		}
	}

	// Rate limit config.
	if rl, ok := cfg["rate_limit"].(map[string]interface{}); ok {
		p.cfg.RateLimit = &RateLimitConfig{
			MaxRegistrations: 10,
			Window:           time.Hour,
		}
		if v, ok := rl["max_registrations"].(float64); ok {
			p.cfg.RateLimit.MaxRegistrations = int(v)
		}
		if v, ok := rl["max_registrations"].(int); ok {
			p.cfg.RateLimit.MaxRegistrations = v
		}
		if v, ok := rl["window"].(string); ok {
			if d, err := time.ParseDuration(v); err == nil {
				p.cfg.RateLimit.Window = d
			}
		}
	}

	p.webhook = NewWebhookClient(p.cfg.ProvisioningWebhook, p.cfg.WebhookSecret, p.cfg.WebhookTimeout)

	slog.Info("agent_onboarding plugin initialized",
		"webhook", p.cfg.ProvisioningWebhook,
		"require_identity", p.cfg.RequireIdentity,
	)
	return nil
}

func (p *Plugin) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Handle registration endpoint.
			if r.URL.Path == "/agent/register" && r.Method == http.MethodPost {
				p.handleRegister(w, r)
				return
			}

			// For all other requests, check if the agent is authenticated.
			// If not, return a helpful 401 with registration info.
			if p.shouldReturn401(r) {
				p.writeAuthRequired(w)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func (p *Plugin) Close() error { return nil }

// handleRegister processes POST /agent/register.
func (p *Plugin) handleRegister(w http.ResponseWriter, r *http.Request) {
	// Rate limit check.
	if p.cfg.RateLimit != nil {
		ip := clientIP(r)
		if !p.allowRequest(ip) {
			plugins.WriteError(w, http.StatusTooManyRequests, "rate_limit_exceeded",
				"Too many registration attempts. Try again later.")
			return
		}
	}

	// Parse request body.
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		plugins.WriteError(w, http.StatusBadRequest, "invalid_request", "Failed to read request body")
		return
	}

	var req RegistrationRequest
	if err := json.Unmarshal(body, &req); err != nil {
		plugins.WriteError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON in request body")
		return
	}

	// Validate required fields.
	if req.AgentID == "" {
		plugins.WriteError(w, http.StatusBadRequest, "missing_field", "agent_id is required")
		return
	}
	if req.AgentName == "" {
		plugins.WriteError(w, http.StatusBadRequest, "missing_field", "agent_name is required")
		return
	}
	if req.AgentProvider == "" {
		plugins.WriteError(w, http.StatusBadRequest, "missing_field", "agent_provider is required")
		return
	}

	// Check identity requirement.
	if p.cfg.RequireIdentity && req.IdentityToken == "" {
		plugins.WriteError(w, http.StatusBadRequest, "identity_required",
			"This API requires an identity_token for registration")
		return
	}

	// Check allowed providers.
	if len(p.cfg.AllowedProviders) > 0 {
		allowed := false
		for _, ap := range p.cfg.AllowedProviders {
			if strings.EqualFold(ap, req.AgentProvider) {
				allowed = true
				break
			}
		}
		if !allowed {
			plugins.WriteError(w, http.StatusForbidden, "provider_not_allowed",
				fmt.Sprintf("Agent provider %q is not allowed", req.AgentProvider))
			return
		}
	}

	// Build webhook request.
	webhookReq := &WebhookRequest{
		AgentID:          req.AgentID,
		AgentName:        req.AgentName,
		AgentProvider:    req.AgentProvider,
		IdentityVerified: req.IdentityToken != "",
		RequestIP:        clientIP(r),
		Timestamp:        NowFunc().UTC().Format(time.RFC3339),
	}

	// Call provisioning webhook.
	resp, err := p.webhook.Call(webhookReq)
	if err != nil {
		slog.Error("provisioning webhook failed", "error", err, "agent_id", req.AgentID)
		plugins.WriteError(w, http.StatusBadGateway, "webhook_error",
			"Failed to provision credentials. Please try again later.")
		return
	}

	slog.Info("agent registration processed",
		"agent_id", req.AgentID,
		"provider", req.AgentProvider,
		"status", resp.Status,
	)

	// Return webhook response to agent.
	w.Header().Set("Content-Type", "application/json")
	if resp.Status == "rejected" {
		w.WriteHeader(http.StatusForbidden)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	json.NewEncoder(w).Encode(resp)
}

// shouldReturn401 checks if the request lacks authentication and should get a 401.
// We only intercept if no auth credentials are present at all.
func (p *Plugin) shouldReturn401(r *http.Request) bool {
	// Don't block discovery/well-known paths.
	path := r.URL.Path
	if strings.HasPrefix(path, "/.well-known/") ||
		path == "/llms.txt" || path == "/llms-full.txt" ||
		path == "/agents.txt" || path == "/robots.txt" ||
		path == "/agent/register" {
		return false
	}

	// Check for any auth header.
	if r.Header.Get("Authorization") != "" {
		return false
	}
	if r.Header.Get("X-API-Key") != "" {
		return false
	}
	// Check query param api_key.
	if r.URL.Query().Get("api_key") != "" {
		return false
	}

	return true
}

// writeAuthRequired sends a 401 with registration info.
func (p *Plugin) writeAuthRequired(w http.ResponseWriter) {
	resp := AuthRequiredResponse{
		Error:                    "auth_required",
		Message:                  "This API requires authentication. Register to get credentials.",
		RegisterURL:              "/agent/register",
		AuthDocs:                 p.cfg.AuthDocs,
		SupportedCredentialTypes: SupportedCredentialTypes,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(resp)
}

// allowRequest checks per-IP rate limiting for registration.
func (p *Plugin) allowRequest(ip string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := NowFunc()
	win, exists := p.windows[ip]
	if !exists || now.After(win.resetAt) {
		p.windows[ip] = &rateLimitWindow{
			count:   1,
			resetAt: now.Add(p.cfg.RateLimit.Window),
		}
		return true
	}

	if win.count >= p.cfg.RateLimit.MaxRegistrations {
		return false
	}
	win.count++
	return true
}

// clientIP extracts the client IP from the request.
func clientIP(r *http.Request) string {
	// Check X-Forwarded-For first.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	// Check X-Real-IP.
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Fall back to RemoteAddr.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

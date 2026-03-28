// Package security implements CORS, security headers, and robots.txt generation.
// Ported from agent-layer-ts security-headers.ts and robots-txt.ts.
package security

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/lightlayer-dev/gateway/internal/plugins"
)

func init() {
	plugins.Register("security", func() plugins.Plugin { return New() })
}

// AIAgents is the list of well-known AI agent User-Agent strings.
// Ported from agent-layer-ts robots-txt.ts AI_AGENTS.
var AIAgents = []string{
	"GPTBot",
	"ChatGPT-User",
	"Google-Extended",
	"Anthropic",
	"ClaudeBot",
	"CCBot",
	"Amazonbot",
	"Bytespider",
	"Applebot-Extended",
	"PerplexityBot",
	"Cohere-ai",
}

// ── Configuration ──────────────────────────────────────────────────────

// CORSConfig holds CORS settings.
type CORSConfig struct {
	Origins     []string
	Methods     []string
	Headers     []string
	Credentials bool
	MaxAge      int // seconds
}

// SecurityHeadersConfig holds header generation settings.
// Ported from agent-layer-ts SecurityHeadersConfig.
type SecurityHeadersConfig struct {
	HSTSMaxAge            int
	HSTSIncludeSubdomains bool
	FrameOptions          string // "DENY", "SAMEORIGIN", or "" to disable
	ContentTypeOptions    string // "nosniff" or "" to disable
	ReferrerPolicy        string // e.g. "strict-origin-when-cross-origin" or "" to disable
	CSP                   string // e.g. "default-src 'self'" or "" to disable
	PermissionsPolicy     string // optional, "" to omit
}

// RobotsTxtRule defines a single robots.txt rule block.
type RobotsTxtRule struct {
	UserAgent  string
	Allow      []string
	Disallow   []string
	CrawlDelay int
}

// RobotsTxtConfig controls robots.txt generation.
// Ported from agent-layer-ts RobotsTxtConfig.
type RobotsTxtConfig struct {
	Rules           []RobotsTxtRule
	Sitemaps        []string
	IncludeAIAgents bool
	AIAgentPolicy   string   // "allow" or "disallow"
	AIAllow         []string
	AIDisallow      []string
}

// ── Plugin ─────────────────────────────────────────────────────────────

// Plugin implements CORS handling, security headers, and robots.txt serving.
type Plugin struct {
	cors      CORSConfig
	headers   SecurityHeadersConfig
	robotsTxt RobotsTxtConfig

	// Precomputed values.
	securityHeaders map[string]string
	robotsTxtBody   string
}

// New creates a new security plugin with sensible defaults.
func New() *Plugin {
	return &Plugin{}
}

func (p *Plugin) Name() string { return "security" }

func (p *Plugin) Init(cfg map[string]interface{}) error {
	// Parse CORS config.
	p.cors = CORSConfig{
		Origins: toStringSlice(cfg["cors_origins"]),
		Methods: toStringSlice(cfg["cors_methods"]),
		Headers: toStringSlice(cfg["cors_headers"]),
		MaxAge:  toInt(cfg["cors_max_age"]),
	}
	if v, ok := cfg["cors_credentials"].(bool); ok {
		p.cors.Credentials = v
	}
	if len(p.cors.Methods) == 0 {
		p.cors.Methods = []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"}
	}
	if len(p.cors.Headers) == 0 {
		p.cors.Headers = []string{"Content-Type", "Authorization", "X-Requested-With"}
	}

	// Parse security headers config (match TS defaults exactly).
	p.headers = SecurityHeadersConfig{
		HSTSMaxAge:            31536000,
		HSTSIncludeSubdomains: true,
		FrameOptions:          "DENY",
		ContentTypeOptions:    "nosniff",
		ReferrerPolicy:        "strict-origin-when-cross-origin",
		CSP:                   "default-src 'self'",
	}
	if v := toInt(cfg["hsts_max_age"]); v > 0 {
		p.headers.HSTSMaxAge = v
	} else if _, exists := cfg["hsts_max_age"]; exists {
		p.headers.HSTSMaxAge = toInt(cfg["hsts_max_age"])
	}
	if v, ok := cfg["hsts_include_subdomains"]; ok {
		if b, ok := v.(bool); ok {
			p.headers.HSTSIncludeSubdomains = b
		}
	}
	if v := toString(cfg["frame_options"]); v != "" {
		p.headers.FrameOptions = v
	}
	if v := toString(cfg["content_type_options"]); v != "" {
		p.headers.ContentTypeOptions = v
	}
	if v := toString(cfg["referrer_policy"]); v != "" {
		p.headers.ReferrerPolicy = v
	}
	if v := toString(cfg["csp"]); v != "" {
		p.headers.CSP = v
	}
	if v := toString(cfg["permissions_policy"]); v != "" {
		p.headers.PermissionsPolicy = v
	}

	// Precompute security headers map (matches TS generateSecurityHeaders).
	p.securityHeaders = GenerateSecurityHeaders(p.headers)

	// Parse robots.txt config.
	p.robotsTxt = RobotsTxtConfig{
		IncludeAIAgents: true,
		AIAgentPolicy:   "allow",
		AIAllow:         []string{"/"},
	}
	if rtCfg, ok := cfg["robots_txt"].(map[string]interface{}); ok {
		if v, ok := rtCfg["include_ai_agents"].(bool); ok {
			p.robotsTxt.IncludeAIAgents = v
		}
		if v := toString(rtCfg["ai_agent_policy"]); v != "" {
			p.robotsTxt.AIAgentPolicy = v
		}
		if v := toStringSlice(rtCfg["ai_allow"]); len(v) > 0 {
			p.robotsTxt.AIAllow = v
		}
		if v := toStringSlice(rtCfg["ai_disallow"]); len(v) > 0 {
			p.robotsTxt.AIDisallow = v
		}
		if v := toStringSlice(rtCfg["sitemaps"]); len(v) > 0 {
			p.robotsTxt.Sitemaps = v
		}
		if rules, ok := rtCfg["rules"].([]interface{}); ok {
			for _, r := range rules {
				if rm, ok := r.(map[string]interface{}); ok {
					p.robotsTxt.Rules = append(p.robotsTxt.Rules, RobotsTxtRule{
						UserAgent:  toString(rm["user_agent"]),
						Allow:      toStringSlice(rm["allow"]),
						Disallow:   toStringSlice(rm["disallow"]),
						CrawlDelay: toInt(rm["crawl_delay"]),
					})
				}
			}
		}
	}

	// Precompute robots.txt body.
	p.robotsTxtBody = GenerateRobotsTxt(p.robotsTxt)

	slog.Info("security plugin initialized",
		"cors_origins", p.cors.Origins,
		"hsts_max_age", p.headers.HSTSMaxAge,
		"robots_txt_agents", len(AIAgents),
	)
	return nil
}

func (p *Plugin) Close() error { return nil }

func (p *Plugin) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Serve robots.txt.
			if r.URL.Path == "/robots.txt" {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(p.robotsTxtBody))
				return
			}

			// Set security headers on every response.
			for k, v := range p.securityHeaders {
				w.Header().Set(k, v)
			}

			// CORS handling.
			origin := r.Header.Get("Origin")
			if origin != "" && p.originAllowed(origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				if p.cors.Credentials {
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}

				// Preflight request.
				if r.Method == http.MethodOptions {
					w.Header().Set("Access-Control-Allow-Methods", strings.Join(p.cors.Methods, ", "))
					w.Header().Set("Access-Control-Allow-Headers", strings.Join(p.cors.Headers, ", "))
					if p.cors.MaxAge > 0 {
						w.Header().Set("Access-Control-Max-Age", fmt.Sprintf("%d", p.cors.MaxAge))
					}
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// originAllowed checks if the given origin is in the allowed list.
func (p *Plugin) originAllowed(origin string) bool {
	if len(p.cors.Origins) == 0 {
		return false
	}
	for _, o := range p.cors.Origins {
		if o == "*" || o == origin {
			return true
		}
	}
	return false
}

// ── Security Headers Generator ─────────────────────────────────────────

// GenerateSecurityHeaders produces a map of security headers from config.
// Port of agent-layer-ts generateSecurityHeaders().
func GenerateSecurityHeaders(cfg SecurityHeadersConfig) map[string]string {
	headers := make(map[string]string)

	// HSTS
	if cfg.HSTSMaxAge > 0 {
		v := fmt.Sprintf("max-age=%d", cfg.HSTSMaxAge)
		if cfg.HSTSIncludeSubdomains {
			v += "; includeSubDomains"
		}
		headers["Strict-Transport-Security"] = v
	}

	// X-Content-Type-Options
	if cfg.ContentTypeOptions != "" {
		headers["X-Content-Type-Options"] = cfg.ContentTypeOptions
	}

	// X-Frame-Options
	if cfg.FrameOptions != "" {
		headers["X-Frame-Options"] = cfg.FrameOptions
	}

	// Referrer-Policy
	if cfg.ReferrerPolicy != "" {
		headers["Referrer-Policy"] = cfg.ReferrerPolicy
	}

	// CSP
	if cfg.CSP != "" {
		headers["Content-Security-Policy"] = cfg.CSP
	}

	// Permissions-Policy
	if cfg.PermissionsPolicy != "" {
		headers["Permissions-Policy"] = cfg.PermissionsPolicy
	}

	return headers
}

// ── robots.txt Generator ───────────────────────────────────────────────

// GenerateRobotsTxt produces a robots.txt string with AI agent awareness.
// Port of agent-layer-ts generateRobotsTxt().
func GenerateRobotsTxt(cfg RobotsTxtConfig) string {
	var lines []string

	if len(cfg.Rules) > 0 {
		// Use explicit rules.
		for _, rule := range cfg.Rules {
			lines = append(lines, fmt.Sprintf("User-agent: %s", rule.UserAgent))
			for _, path := range rule.Allow {
				lines = append(lines, fmt.Sprintf("Allow: %s", path))
			}
			for _, path := range rule.Disallow {
				lines = append(lines, fmt.Sprintf("Disallow: %s", path))
			}
			if rule.CrawlDelay > 0 {
				lines = append(lines, fmt.Sprintf("Crawl-delay: %d", rule.CrawlDelay))
			}
			lines = append(lines, "")
		}
	} else {
		// Generate defaults.
		lines = append(lines, "User-agent: *")
		lines = append(lines, "Allow: /")
		lines = append(lines, "")
	}

	// Add AI agent rules if requested (default: true).
	if cfg.IncludeAIAgents && len(cfg.Rules) == 0 {
		aiAllow := cfg.AIAllow
		if len(aiAllow) == 0 {
			aiAllow = []string{"/"}
		}

		for _, agent := range AIAgents {
			lines = append(lines, fmt.Sprintf("User-agent: %s", agent))
			if cfg.AIAgentPolicy == "disallow" {
				lines = append(lines, "Disallow: /")
			} else {
				for _, path := range aiAllow {
					lines = append(lines, fmt.Sprintf("Allow: %s", path))
				}
				for _, path := range cfg.AIDisallow {
					lines = append(lines, fmt.Sprintf("Disallow: %s", path))
				}
			}
			lines = append(lines, "")
		}
	}

	// Add sitemaps.
	for _, sitemap := range cfg.Sitemaps {
		lines = append(lines, fmt.Sprintf("Sitemap: %s", sitemap))
	}

	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}

// ── Helpers ────────────────────────────────────────────────────────────

func toString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	}
	return 0
}

func toStringSlice(v interface{}) []string {
	if v == nil {
		return nil
	}
	if ss, ok := v.([]string); ok {
		return ss
	}
	if items, ok := v.([]interface{}); ok {
		out := make([]string, 0, len(items))
		for _, item := range items {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

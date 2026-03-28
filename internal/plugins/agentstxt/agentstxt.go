// Package agentstxt implements an agents.txt access control plugin.
// Ported from agent-layer-ts agents-txt.ts.
//
// agents.txt is a robots.txt-style permission and capability declaration for AI agents.
// It tells agents what paths they can access, rate limits, auth requirements, and
// preferred interface.
package agentstxt

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/lightlayer-dev/gateway/internal/plugins"
)

func init() {
	plugins.Register("agents_txt", func() plugins.Plugin { return New() })
}

// AgentsTxtRateLimit declares a rate limit in agents.txt.
type AgentsTxtRateLimit struct {
	Max           int
	WindowSeconds int // default: 60
}

// AgentsTxtAuth declares auth requirements.
type AgentsTxtAuth struct {
	Type     string // bearer, api_key, oauth2, none
	Endpoint string
	DocsURL  string
}

// AgentsTxtRule is a single rule block in agents.txt.
type AgentsTxtRule struct {
	Agent              string
	Allow              []string
	Deny               []string
	RateLimit          *AgentsTxtRateLimit
	PreferredInterface string // rest, mcp, graphql, a2a
	Auth               *AgentsTxtAuth
	Description        string
}

// AgentsTxtConfig is the top-level agents.txt configuration.
type AgentsTxtConfig struct {
	Rules        []AgentsTxtRule
	SiteName     string
	Contact      string
	DiscoveryURL string
}

// Plugin serves /agents.txt and enforces per-agent access rules.
type Plugin struct {
	cfg     AgentsTxtConfig
	content string // cached agents.txt content
}

// New creates a new agents.txt plugin.
func New() *Plugin {
	return &Plugin{}
}

func (p *Plugin) Name() string { return "agents_txt" }

func (p *Plugin) Init(cfg map[string]interface{}) error {
	if sn, ok := cfg["site_name"].(string); ok {
		p.cfg.SiteName = sn
	}
	if c, ok := cfg["contact"].(string); ok {
		p.cfg.Contact = c
	}
	if du, ok := cfg["discovery_url"].(string); ok {
		p.cfg.DiscoveryURL = du
	}

	if rules, ok := cfg["rules"].([]interface{}); ok {
		for _, ri := range rules {
			rm, ok := ri.(map[string]interface{})
			if !ok {
				continue
			}
			rule := AgentsTxtRule{}
			if a, ok := rm["agent"].(string); ok {
				rule.Agent = a
			}
			if allow, ok := rm["allow"].([]interface{}); ok {
				for _, v := range allow {
					if s, ok := v.(string); ok {
						rule.Allow = append(rule.Allow, s)
					}
				}
			}
			if deny, ok := rm["deny"].([]interface{}); ok {
				for _, v := range deny {
					if s, ok := v.(string); ok {
						rule.Deny = append(rule.Deny, s)
					}
				}
			}
			if rl, ok := rm["rate_limit"].(map[string]interface{}); ok {
				rule.RateLimit = &AgentsTxtRateLimit{
					Max:           toInt(rl["max"]),
					WindowSeconds: toInt(rl["window_seconds"]),
				}
				if rule.RateLimit.WindowSeconds <= 0 {
					rule.RateLimit.WindowSeconds = 60
				}
			}
			if pi, ok := rm["preferred_interface"].(string); ok {
				rule.PreferredInterface = pi
			}
			if auth, ok := rm["auth"].(map[string]interface{}); ok {
				rule.Auth = &AgentsTxtAuth{}
				if t, ok := auth["type"].(string); ok {
					rule.Auth.Type = t
				}
				if e, ok := auth["endpoint"].(string); ok {
					rule.Auth.Endpoint = e
				}
				if d, ok := auth["docs_url"].(string); ok {
					rule.Auth.DocsURL = d
				}
			}
			if d, ok := rm["description"].(string); ok {
				rule.Description = d
			}
			p.cfg.Rules = append(p.cfg.Rules, rule)
		}
	}

	p.content = GenerateAgentsTxt(p.cfg)

	slog.Info("agents_txt plugin initialized", "rules", len(p.cfg.Rules))
	return nil
}

func (p *Plugin) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Serve /agents.txt endpoint.
			if r.URL.Path == "/agents.txt" {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(p.content))
				return
			}

			// If no rules configured, pass through.
			if len(p.cfg.Rules) == 0 {
				next.ServeHTTP(w, r)
				return
			}

			// Enforce access rules.
			agentName := resolveAgentName(r)
			allowed := IsAgentAllowed(p.cfg.Rules, agentName, r.URL.Path)

			if allowed != nil && !*allowed {
				plugins.WriteError(w, http.StatusForbidden, "agent_denied",
					fmt.Sprintf("Agent %q is not allowed to access %s", agentName, r.URL.Path))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func (p *Plugin) Close() error { return nil }

// resolveAgentName extracts the agent name from the request context.
func resolveAgentName(r *http.Request) string {
	rc := plugins.GetRequestContext(r.Context())
	if rc != nil && rc.AgentInfo != nil && rc.AgentInfo.Detected && rc.AgentInfo.Name != "" {
		return rc.AgentInfo.Name
	}
	return ""
}

// GenerateAgentsTxt produces agents.txt file content from configuration.
func GenerateAgentsTxt(cfg AgentsTxtConfig) string {
	var lines []string

	lines = append(lines, "# agents.txt — AI Agent Access Policy")

	if cfg.SiteName != "" {
		lines = append(lines, fmt.Sprintf("# Site: %s", cfg.SiteName))
	}
	if cfg.Contact != "" {
		lines = append(lines, fmt.Sprintf("# Contact: %s", cfg.Contact))
	}
	if cfg.DiscoveryURL != "" {
		lines = append(lines, fmt.Sprintf("# Discovery: %s", cfg.DiscoveryURL))
	}

	for _, rule := range cfg.Rules {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("User-agent: %s", rule.Agent))

		if rule.Description != "" {
			lines = append(lines, fmt.Sprintf("# %s", rule.Description))
		}

		for _, path := range rule.Allow {
			lines = append(lines, fmt.Sprintf("Allow: %s", path))
		}
		for _, path := range rule.Deny {
			lines = append(lines, fmt.Sprintf("Deny: %s", path))
		}

		if rule.RateLimit != nil {
			ws := rule.RateLimit.WindowSeconds
			if ws <= 0 {
				ws = 60
			}
			lines = append(lines, fmt.Sprintf("Rate-limit: %d/%ds", rule.RateLimit.Max, ws))
		}

		if rule.PreferredInterface != "" {
			lines = append(lines, fmt.Sprintf("Preferred-interface: %s", rule.PreferredInterface))
		}

		if rule.Auth != nil {
			authParts := []string{rule.Auth.Type}
			if rule.Auth.Endpoint != "" {
				authParts = append(authParts, rule.Auth.Endpoint)
			}
			lines = append(lines, fmt.Sprintf("Auth: %s", strings.Join(authParts, " ")))
			if rule.Auth.DocsURL != "" {
				lines = append(lines, fmt.Sprintf("Auth-docs: %s", rule.Auth.DocsURL))
			}
		}
	}

	return strings.Join(lines, "\n") + "\n"
}

// ParseAgentsTxt parses an agents.txt string back into structured rules.
func ParseAgentsTxt(content string) AgentsTxtConfig {
	lines := strings.Split(content, "\n")
	cfg := AgentsTxtConfig{}
	var currentRule *AgentsTxtRule

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)

		// Header comments.
		if strings.HasPrefix(line, "# Site:") {
			cfg.SiteName = strings.TrimSpace(strings.TrimPrefix(line, "# Site:"))
			continue
		}
		if strings.HasPrefix(line, "# Contact:") {
			cfg.Contact = strings.TrimSpace(strings.TrimPrefix(line, "# Contact:"))
			continue
		}
		if strings.HasPrefix(line, "# Discovery:") {
			cfg.DiscoveryURL = strings.TrimSpace(strings.TrimPrefix(line, "# Discovery:"))
			continue
		}

		// Skip comments and blanks outside rule blocks.
		if line == "" || (strings.HasPrefix(line, "#") && currentRule == nil) {
			continue
		}
		// Skip inline comments within rule blocks.
		if strings.HasPrefix(line, "#") && currentRule != nil {
			continue
		}

		colonIdx := strings.Index(line, ":")
		if colonIdx == -1 {
			continue
		}

		directive := strings.TrimSpace(strings.ToLower(line[:colonIdx]))
		value := strings.TrimSpace(line[colonIdx+1:])

		if directive == "user-agent" {
			rule := AgentsTxtRule{Agent: value}
			cfg.Rules = append(cfg.Rules, rule)
			currentRule = &cfg.Rules[len(cfg.Rules)-1]
			continue
		}

		if currentRule == nil {
			continue
		}

		switch directive {
		case "allow":
			currentRule.Allow = append(currentRule.Allow, value)
		case "deny":
			currentRule.Deny = append(currentRule.Deny, value)
		case "rate-limit":
			// Format: "100/60s"
			var max, ws int
			if n, _ := fmt.Sscanf(value, "%d/%ds", &max, &ws); n == 2 {
				currentRule.RateLimit = &AgentsTxtRateLimit{Max: max, WindowSeconds: ws}
			}
		case "preferred-interface":
			if value == "rest" || value == "mcp" || value == "graphql" || value == "a2a" {
				currentRule.PreferredInterface = value
			}
		case "auth":
			parts := strings.Fields(value)
			auth := &AgentsTxtAuth{Type: parts[0]}
			if len(parts) > 1 {
				auth.Endpoint = parts[1]
			}
			currentRule.Auth = auth
		case "auth-docs":
			if currentRule.Auth != nil {
				currentRule.Auth.DocsURL = value
			}
		}
	}

	return cfg
}

// IsAgentAllowed checks whether a given agent+path combination is allowed.
// Returns nil if no matching rule exists.
// Priority: exact match > prefix pattern > wildcard.
// Within a rule: deny takes precedence over allow.
func IsAgentAllowed(rules []AgentsTxtRule, agentName string, path string) *bool {
	rule := findMatchingRule(rules, agentName)
	if rule == nil {
		return nil
	}

	// Check deny first (deny takes precedence).
	for _, pattern := range rule.Deny {
		if pathMatches(path, pattern) {
			f := false
			return &f
		}
	}

	// Check allow.
	if len(rule.Allow) > 0 {
		for _, pattern := range rule.Allow {
			if pathMatches(path, pattern) {
				t := true
				return &t
			}
		}
		// Allow rules exist but none matched → deny.
		f := false
		return &f
	}

	// No allow/deny rules → implicitly allowed.
	t := true
	return &t
}

// FindMatchingRule finds the best matching rule for an agent name.
// Priority: exact match > prefix pattern > wildcard.
func findMatchingRule(rules []AgentsTxtRule, agentName string) *AgentsTxtRule {
	var wildcardRule *AgentsTxtRule
	var patternRule *AgentsTxtRule
	var exactRule *AgentsTxtRule

	for i := range rules {
		rule := &rules[i]
		if rule.Agent == "*" {
			wildcardRule = rule
		} else if strings.HasSuffix(rule.Agent, "*") {
			prefix := strings.TrimSuffix(rule.Agent, "*")
			if strings.HasPrefix(agentName, prefix) {
				patternRule = rule
			}
		} else if rule.Agent == agentName {
			exactRule = rule
		}
	}

	if exactRule != nil {
		return exactRule
	}
	if patternRule != nil {
		return patternRule
	}
	return wildcardRule
}

// pathMatches performs simple glob-style path matching.
// Supports trailing * for prefix matching.
func pathMatches(path, pattern string) bool {
	if pattern == "*" || pattern == "/*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(path, prefix)
	}
	return path == pattern
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

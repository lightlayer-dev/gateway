// Package discovery implements unified multi-format discovery.
//
// A single UnifiedDiscoveryConfig generates all five discovery formats:
//   - /.well-known/ai       → AI manifest JSON
//   - /.well-known/agent.json → Google A2A Agent Card
//   - /agents.txt            → per-agent permission blocks
//   - /llms.txt              → human-readable API description
//   - /llms-full.txt         → auto-generated from route metadata
//
// Ported from agent-layer-ts unified-discovery.ts, a2a.ts, llms-txt.ts.
package discovery

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/lightlayer-dev/gateway/internal/plugins"
)

func init() {
	plugins.Register("discovery", func() plugins.Plugin { return New() })
}

// ── A2A Agent Card Types ────────────────────────────────────────────────

// A2ASkill describes a capability the agent can perform.
type A2ASkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Examples    []string `json:"examples,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`
}

// A2AAuthScheme describes authentication the agent supports.
type A2AAuthScheme struct {
	Type             string            `json:"type"`
	In               string            `json:"in,omitempty"`
	Name             string            `json:"name,omitempty"`
	AuthorizationURL string            `json:"authorizationUrl,omitempty"`
	TokenURL         string            `json:"tokenUrl,omitempty"`
	Scopes           map[string]string `json:"scopes,omitempty"`
}

// A2AProvider holds organization info.
type A2AProvider struct {
	Organization string `json:"organization"`
	URL          string `json:"url,omitempty"`
}

// A2ACapabilities describes what the agent supports.
type A2ACapabilities struct {
	Streaming              bool `json:"streaming,omitempty"`
	PushNotifications      bool `json:"pushNotifications,omitempty"`
	StateTransitionHistory bool `json:"stateTransitionHistory,omitempty"`
}

// A2AAgentCard is the full document served at /.well-known/agent.json.
type A2AAgentCard struct {
	ProtocolVersion    string           `json:"protocolVersion"`
	Name               string           `json:"name"`
	Description        string           `json:"description,omitempty"`
	URL                string           `json:"url"`
	Provider           *A2AProvider     `json:"provider,omitempty"`
	Version            string           `json:"version,omitempty"`
	DocumentationURL   string           `json:"documentationUrl,omitempty"`
	Capabilities       *A2ACapabilities `json:"capabilities,omitempty"`
	Authentication     *A2AAuthScheme   `json:"authentication,omitempty"`
	DefaultInputModes  []string         `json:"defaultInputModes,omitempty"`
	DefaultOutputModes []string         `json:"defaultOutputModes,omitempty"`
	Skills             []A2ASkill       `json:"skills"`
}

// ── AI Manifest Types ───────────────────────────────────────────────────

// AIManifestAuth describes authentication in the AI manifest.
type AIManifestAuth struct {
	Type             string            `json:"type"`
	AuthorizationURL string            `json:"authorization_url,omitempty"`
	TokenURL         string            `json:"token_url,omitempty"`
	Scopes           map[string]string `json:"scopes,omitempty"`
}

// AIManifest is the /.well-known/ai document.
type AIManifest struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	OpenAPIURL   string          `json:"openapi_url,omitempty"`
	LlmsTxtURL   string          `json:"llms_txt_url,omitempty"`
	Auth         *AIManifestAuth `json:"auth,omitempty"`
	Contact      *ContactInfo    `json:"contact,omitempty"`
	Capabilities []string        `json:"capabilities,omitempty"`
}

// ContactInfo holds contact details.
type ContactInfo struct {
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

// ── Route Metadata (for llms-full.txt) ──────────────────────────────────

// RouteParameter describes a single API parameter.
type RouteParameter struct {
	Name        string `json:"name"`
	In          string `json:"in"` // path, query, header, body
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
}

// RouteMetadata describes an API endpoint for llms-full.txt generation.
type RouteMetadata struct {
	Method      string           `json:"method"`
	Path        string           `json:"path"`
	Summary     string           `json:"summary,omitempty"`
	Description string           `json:"description,omitempty"`
	Parameters  []RouteParameter `json:"parameters,omitempty"`
}

// ── Agents.txt Types ────────────────────────────────────────────────────

// AgentsTxtRule is a single allow/disallow rule.
type AgentsTxtRule struct {
	Path       string `json:"path"`
	Permission string `json:"permission"` // "allow" or "disallow"
}

// AgentsTxtBlock targets a user-agent with rules.
type AgentsTxtBlock struct {
	UserAgent string          `json:"user_agent"`
	Rules     []AgentsTxtRule `json:"rules"`
}

// AgentsTxtConfig configures agents.txt generation.
type AgentsTxtConfig struct {
	Blocks     []AgentsTxtBlock `json:"blocks"`
	SitemapURL string           `json:"sitemap_url,omitempty"`
	Comment    string           `json:"comment,omitempty"`
}

// ── Unified Config ──────────────────────────────────────────────────────

// DiscoveryFormats controls which formats are generated.
type DiscoveryFormats struct {
	WellKnownAI *bool `json:"well_known_ai,omitempty"`
	AgentCard   *bool `json:"agent_card,omitempty"`
	AgentsTxt   *bool `json:"agents_txt,omitempty"`
	LlmsTxt     *bool `json:"llms_txt,omitempty"`
}

// UnifiedAuthConfig is auth shared across formats.
type UnifiedAuthConfig struct {
	Type             string            `json:"type"` // oauth2, api_key, bearer, none
	In               string            `json:"in,omitempty"`
	Name             string            `json:"name,omitempty"`
	AuthorizationURL string            `json:"authorization_url,omitempty"`
	TokenURL         string            `json:"token_url,omitempty"`
	Scopes           map[string]string `json:"scopes,omitempty"`
}

// UnifiedSkill maps to A2A skills, llms.txt sections, AI manifest capabilities.
type UnifiedSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Examples    []string `json:"examples,omitempty"`
	InputModes  []string `json:"input_modes,omitempty"`
	OutputModes []string `json:"output_modes,omitempty"`
}

// LlmsTxtSection is an extra section appended to llms.txt.
type LlmsTxtSection struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

// UnifiedDiscoveryConfig is the single source of truth for all discovery formats.
type UnifiedDiscoveryConfig struct {
	Name              string            `json:"name"`
	Description       string            `json:"description,omitempty"`
	URL               string            `json:"url"`
	Version           string            `json:"version,omitempty"`
	Provider          *A2AProvider      `json:"provider,omitempty"`
	Contact           *ContactInfo      `json:"contact,omitempty"`
	OpenAPIURL        string            `json:"openapi_url,omitempty"`
	DocumentationURL  string            `json:"documentation_url,omitempty"`
	Capabilities      []string          `json:"capabilities,omitempty"`
	AgentCapabilities *A2ACapabilities  `json:"agent_capabilities,omitempty"`
	Auth              *UnifiedAuthConfig `json:"auth,omitempty"`
	Skills            []UnifiedSkill    `json:"skills,omitempty"`
	Routes            []RouteMetadata   `json:"routes,omitempty"`
	AgentsTxt         *AgentsTxtConfig  `json:"agents_txt,omitempty"`
	Formats           *DiscoveryFormats `json:"formats,omitempty"`
	LlmsTxtSections   []LlmsTxtSection  `json:"llms_txt_sections,omitempty"`
}

// ── Generators ──────────────────────────────────────────────────────────

// isFormatEnabled checks if a format is enabled (defaults to true).
func isFormatEnabled(formats *DiscoveryFormats, format string) bool {
	if formats == nil {
		return true
	}
	switch format {
	case "wellKnownAi":
		if formats.WellKnownAI == nil {
			return true
		}
		return *formats.WellKnownAI
	case "agentCard":
		if formats.AgentCard == nil {
			return true
		}
		return *formats.AgentCard
	case "agentsTxt":
		if formats.AgentsTxt == nil {
			return true
		}
		return *formats.AgentsTxt
	case "llmsTxt":
		if formats.LlmsTxt == nil {
			return true
		}
		return *formats.LlmsTxt
	default:
		return true
	}
}

// generateAIManifest produces the /.well-known/ai JSON.
func generateAIManifest(cfg *UnifiedDiscoveryConfig) *AIManifest {
	m := &AIManifest{
		Name:         cfg.Name,
		Description:  cfg.Description,
		OpenAPIURL:   cfg.OpenAPIURL,
		Contact:      cfg.Contact,
		Capabilities: cfg.Capabilities,
	}

	if isFormatEnabled(cfg.Formats, "llmsTxt") {
		m.LlmsTxtURL = cfg.URL + "/llms.txt"
	}

	if cfg.Auth != nil {
		authType := cfg.Auth.Type
		if authType == "bearer" {
			authType = "api_key"
		}
		m.Auth = &AIManifestAuth{
			Type:             authType,
			AuthorizationURL: cfg.Auth.AuthorizationURL,
			TokenURL:         cfg.Auth.TokenURL,
			Scopes:           cfg.Auth.Scopes,
		}
	}

	return m
}

// generateAgentCard produces the /.well-known/agent.json A2A Agent Card.
func generateAgentCard(cfg *UnifiedDiscoveryConfig) *A2AAgentCard {
	card := &A2AAgentCard{
		ProtocolVersion:    "1.0.0",
		Name:               cfg.Name,
		Description:        cfg.Description,
		URL:                cfg.URL,
		Provider:           cfg.Provider,
		Version:            cfg.Version,
		Capabilities:       cfg.AgentCapabilities,
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Skills:             make([]A2ASkill, 0, len(cfg.Skills)),
	}

	docURL := cfg.DocumentationURL
	if docURL == "" {
		docURL = cfg.OpenAPIURL
	}
	card.DocumentationURL = docURL

	if cfg.Auth != nil {
		authType := cfg.Auth.Type
		if authType == "api_key" {
			authType = "apiKey"
		}
		card.Authentication = &A2AAuthScheme{
			Type:             authType,
			In:               cfg.Auth.In,
			Name:             cfg.Auth.Name,
			AuthorizationURL: cfg.Auth.AuthorizationURL,
			TokenURL:         cfg.Auth.TokenURL,
			Scopes:           cfg.Auth.Scopes,
		}
	}

	for _, s := range cfg.Skills {
		card.Skills = append(card.Skills, A2ASkill{
			ID:          s.ID,
			Name:        s.Name,
			Description: s.Description,
			Tags:        s.Tags,
			Examples:    s.Examples,
			InputModes:  s.InputModes,
			OutputModes: s.OutputModes,
		})
	}

	return card
}

// generateLlmsTxt produces /llms.txt content.
func generateLlmsTxt(cfg *UnifiedDiscoveryConfig) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n", cfg.Name)

	if cfg.Description != "" {
		fmt.Fprintf(&b, "\n> %s\n", cfg.Description)
	}

	// Skills as sections
	for _, s := range cfg.Skills {
		fmt.Fprintf(&b, "\n## %s\n\n", s.Name)
		if s.Description != "" {
			b.WriteString(s.Description)
			b.WriteByte('\n')
		}
		if len(s.Examples) > 0 {
			b.WriteString("\nExamples:\n")
			for _, ex := range s.Examples {
				fmt.Fprintf(&b, "- %s\n", ex)
			}
		}
	}

	// Extra sections
	for _, sec := range cfg.LlmsTxtSections {
		fmt.Fprintf(&b, "\n## %s\n\n%s\n", sec.Title, sec.Content)
	}

	return b.String()
}

// generateLlmsFullTxt produces /llms-full.txt with route documentation.
func generateLlmsFullTxt(cfg *UnifiedDiscoveryConfig) string {
	var b strings.Builder

	// Base content same as llms.txt
	fmt.Fprintf(&b, "# %s\n", cfg.Name)

	if cfg.Description != "" {
		fmt.Fprintf(&b, "\n> %s\n", cfg.Description)
	}

	for _, s := range cfg.Skills {
		fmt.Fprintf(&b, "\n## %s\n\n", s.Name)
		if s.Description != "" {
			b.WriteString(s.Description)
			b.WriteByte('\n')
		}
		if len(s.Examples) > 0 {
			b.WriteString("\nExamples:\n")
			for _, ex := range s.Examples {
				fmt.Fprintf(&b, "- %s\n", ex)
			}
		}
	}

	for _, sec := range cfg.LlmsTxtSections {
		fmt.Fprintf(&b, "\n## %s\n\n%s\n", sec.Title, sec.Content)
	}

	// Auto-generated route documentation
	if len(cfg.Routes) > 0 {
		b.WriteString("\n## API Endpoints\n")

		for _, route := range cfg.Routes {
			fmt.Fprintf(&b, "\n### %s %s\n", strings.ToUpper(route.Method), route.Path)

			if route.Summary != "" {
				fmt.Fprintf(&b, "\n%s\n", route.Summary)
			}
			if route.Description != "" {
				fmt.Fprintf(&b, "\n%s\n", route.Description)
			}
			if len(route.Parameters) > 0 {
				b.WriteString("\n**Parameters:**\n")
				for _, p := range route.Parameters {
					req := ""
					if p.Required {
						req = " (required)"
					}
					desc := ""
					if p.Description != "" {
						desc = " — " + p.Description
					}
					fmt.Fprintf(&b, "- `%s` (%s)%s%s\n", p.Name, p.In, req, desc)
				}
			}
		}
	}

	return b.String()
}

// generateAgentsTxt produces /agents.txt content.
func generateAgentsTxt(cfg *UnifiedDiscoveryConfig) string {
	atCfg := cfg.AgentsTxt
	if atCfg == nil {
		return fmt.Sprintf(
			"# agents.txt — AI agent access rules for %s\n# See https://github.com/nichochar/open-agent-schema\n\nUser-agent: *\nAllow: /\n",
			cfg.Name,
		)
	}

	var lines []string

	if atCfg.Comment != "" {
		for _, line := range strings.Split(atCfg.Comment, "\n") {
			lines = append(lines, "# "+line)
		}
		lines = append(lines, "")
	}

	for _, block := range atCfg.Blocks {
		lines = append(lines, "User-agent: "+block.UserAgent)
		for _, rule := range block.Rules {
			directive := "Allow"
			if rule.Permission == "disallow" {
				directive = "Disallow"
			}
			lines = append(lines, directive+": "+rule.Path)
		}
		lines = append(lines, "")
	}

	if atCfg.SitemapURL != "" {
		lines = append(lines, "Sitemap: "+atCfg.SitemapURL)
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}

// ── Cached endpoint content ─────────────────────────────────────────────

type cachedEndpoint struct {
	body        []byte
	contentType string
}

// ── Plugin ──────────────────────────────────────────────────────────────

// Plugin serves all five discovery endpoints from a unified config.
type Plugin struct {
	mu        sync.RWMutex
	endpoints map[string]*cachedEndpoint
}

// New returns a new discovery Plugin.
func New() *Plugin {
	return &Plugin{
		endpoints: make(map[string]*cachedEndpoint),
	}
}

func (p *Plugin) Name() string { return "discovery" }

// Init parses config and generates all cached endpoints.
func (p *Plugin) Init(cfg map[string]interface{}) error {
	uc, err := parseConfig(cfg)
	if err != nil {
		return fmt.Errorf("discovery: %w", err)
	}
	p.regenerate(uc)
	return nil
}

// Reload regenerates endpoints from a new config.
func (p *Plugin) Reload(cfg map[string]interface{}) error {
	uc, err := parseConfig(cfg)
	if err != nil {
		return fmt.Errorf("discovery reload: %w", err)
	}
	p.regenerate(uc)
	slog.Info("discovery endpoints regenerated")
	return nil
}

func (p *Plugin) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p.mu.RLock()
			ep, ok := p.endpoints[r.URL.Path]
			p.mu.RUnlock()

			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("Content-Type", ep.contentType)
			w.WriteHeader(http.StatusOK)
			w.Write(ep.body)
		})
	}
}

func (p *Plugin) Close() error { return nil }

// regenerate builds all enabled endpoints and caches them.
func (p *Plugin) regenerate(cfg *UnifiedDiscoveryConfig) {
	eps := make(map[string]*cachedEndpoint)

	if isFormatEnabled(cfg.Formats, "wellKnownAi") {
		manifest := generateAIManifest(cfg)
		data, _ := json.MarshalIndent(manifest, "", "  ")
		eps["/.well-known/ai"] = &cachedEndpoint{body: data, contentType: "application/json"}
	}

	if isFormatEnabled(cfg.Formats, "agentCard") {
		card := generateAgentCard(cfg)
		data, _ := json.MarshalIndent(card, "", "  ")
		eps["/.well-known/agent.json"] = &cachedEndpoint{body: data, contentType: "application/json"}
	}

	if isFormatEnabled(cfg.Formats, "llmsTxt") {
		eps["/llms.txt"] = &cachedEndpoint{
			body:        []byte(generateLlmsTxt(cfg)),
			contentType: "text/plain; charset=utf-8",
		}
		eps["/llms-full.txt"] = &cachedEndpoint{
			body:        []byte(generateLlmsFullTxt(cfg)),
			contentType: "text/plain; charset=utf-8",
		}
	}

	if isFormatEnabled(cfg.Formats, "agentsTxt") {
		eps["/agents.txt"] = &cachedEndpoint{
			body:        []byte(generateAgentsTxt(cfg)),
			contentType: "text/plain; charset=utf-8",
		}
	}

	p.mu.Lock()
	p.endpoints = eps
	p.mu.Unlock()
}

// ── Config parsing ──────────────────────────────────────────────────────

// parseConfig converts a generic map into UnifiedDiscoveryConfig.
// Uses JSON round-trip for convenience.
func parseConfig(raw map[string]interface{}) (*UnifiedDiscoveryConfig, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	var cfg UnifiedDiscoveryConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if cfg.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if cfg.URL == "" {
		return nil, fmt.Errorf("url is required")
	}
	return &cfg, nil
}

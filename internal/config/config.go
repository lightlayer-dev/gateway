package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration wraps time.Duration to support YAML duration strings like "30s", "1m", "1h".
type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	if value.Tag == "!!int" || value.Tag == "!!float" {
		// Treat bare numbers as seconds.
		var secs float64
		if err := value.Decode(&secs); err != nil {
			return err
		}
		d.Duration = time.Duration(secs * float64(time.Second))
		return nil
	}
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = parsed
	return nil
}

func (d Duration) MarshalYAML() (interface{}, error) {
	return d.Duration.String(), nil
}

// Config is the top-level gateway configuration.
type Config struct {
	Gateway GatewayConfig `yaml:"gateway"`
	Plugins PluginsConfig `yaml:"plugins"`
	Admin   AdminConfig   `yaml:"admin"`
}

// GatewayConfig holds core proxy settings.
type GatewayConfig struct {
	Listen ListenConfig `yaml:"listen"`
	Origin OriginConfig `yaml:"origin"`
}

// ListenConfig defines the listener address and TLS settings.
type ListenConfig struct {
	Port int       `yaml:"port"`
	Host string    `yaml:"host"`
	TLS  TLSConfig `yaml:"tls,omitempty"`
}

// TLSConfig holds certificate paths for TLS termination.
type TLSConfig struct {
	Cert string `yaml:"cert,omitempty"`
	Key  string `yaml:"key,omitempty"`
}

// OriginConfig defines the upstream origin to proxy to.
type OriginConfig struct {
	URL           string   `yaml:"url"`
	Timeout       Duration `yaml:"timeout"`
	Retries       int      `yaml:"retries,omitempty"`
	TLSSkipVerify bool     `yaml:"tls_skip_verify,omitempty"`
}

// PluginsConfig groups all plugin configurations.
type PluginsConfig struct {
	Discovery       DiscoveryConfig       `yaml:"discovery"`
	Identity        IdentityConfig        `yaml:"identity"`
	Payments        PaymentsConfig        `yaml:"payments"`
	RateLimits      RateLimitsConfig      `yaml:"rate_limits"`
	Analytics       AnalyticsConfig       `yaml:"analytics"`
	Security        SecurityConfig        `yaml:"security"`
	AgentsTxt       AgentsTxtConfig       `yaml:"agents_txt"`
	OAuth2          OAuth2Config          `yaml:"oauth2"`
	MCP             MCPConfig             `yaml:"mcp"`
	APIKeys         APIKeysConfig         `yaml:"api_keys"`
	A2A             A2AConfig             `yaml:"a2a"`
	AgUI            AgUIConfig            `yaml:"ag_ui"`
	AgentOnboarding AgentOnboardingConfig `yaml:"agent_onboarding"`
}

// DiscoveryConfig controls agent discovery endpoint serving.
type DiscoveryConfig struct {
	Enabled      bool         `yaml:"enabled"`
	Name         string       `yaml:"name"`
	Description  string       `yaml:"description"`
	Version      string       `yaml:"version"`
	Capabilities []Capability `yaml:"capabilities,omitempty"`
}

// Capability describes an API capability exposed via discovery.
type Capability struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Methods     []string `yaml:"methods"`
	Paths       []string `yaml:"paths"`
}

// IdentityConfig controls agent identity verification.
type IdentityConfig struct {
	Enabled            bool              `yaml:"enabled"`
	Mode               string            `yaml:"mode"` // log, warn, enforce
	TrustedIssuers     []string          `yaml:"trusted_issuers,omitempty"`
	Audience           []string          `yaml:"audience,omitempty"`
	TrustedDomains     []string          `yaml:"trusted_domains,omitempty"`
	Policies           []AuthzPolicy     `yaml:"policies,omitempty"`
	DefaultPolicy      string            `yaml:"default_policy,omitempty"` // allow, deny
	HeaderName         string            `yaml:"header_name,omitempty"`
	TokenPrefix        string            `yaml:"token_prefix,omitempty"`
	ClockSkewSeconds   int               `yaml:"clock_skew_seconds,omitempty"`
	MaxLifetimeSeconds int               `yaml:"max_lifetime_seconds,omitempty"`
}

// AuthzPolicy defines an authorization rule for agent access.
type AuthzPolicy struct {
	Name           string   `yaml:"name"`
	AgentPattern   string   `yaml:"agent_pattern,omitempty"`
	TrustDomains   []string `yaml:"trust_domains,omitempty"`
	RequiredScopes []string `yaml:"required_scopes,omitempty"`
	Methods        []string `yaml:"methods,omitempty"`
	Paths          []string `yaml:"paths,omitempty"`
	AllowDelegated *bool    `yaml:"allow_delegated,omitempty"`
}

// PaymentsConfig controls x402 payment handling.
type PaymentsConfig struct {
	Enabled     bool           `yaml:"enabled"`
	Facilitator string         `yaml:"facilitator,omitempty"`
	PayTo       string         `yaml:"pay_to,omitempty"`
	Network     string         `yaml:"network,omitempty"`
	Scheme      string         `yaml:"scheme,omitempty"`
	Routes      []PaymentRoute `yaml:"routes,omitempty"`
}

// PaymentRoute defines pricing for a specific path pattern.
type PaymentRoute struct {
	Path               string `yaml:"path"`
	Price              string `yaml:"price"`
	Currency           string `yaml:"currency,omitempty"`
	Network            string `yaml:"network,omitempty"`
	PayTo              string `yaml:"pay_to,omitempty"`
	Scheme             string `yaml:"scheme,omitempty"`
	MaxTimeoutSeconds  int    `yaml:"max_timeout_seconds,omitempty"`
	Description        string `yaml:"description,omitempty"`
}

// RateLimitsConfig controls per-agent rate limiting.
type RateLimitsConfig struct {
	Enabled  bool                 `yaml:"enabled"`
	Default  RateLimit            `yaml:"default"`
	PerAgent map[string]RateLimit `yaml:"per_agent,omitempty"`
}

// RateLimit defines a request count within a time window.
type RateLimit struct {
	Requests int      `yaml:"requests"`
	Window   Duration `yaml:"window"`
}

// AnalyticsConfig controls traffic logging and reporting.
type AnalyticsConfig struct {
	Enabled       bool   `yaml:"enabled"`
	LogFile       string `yaml:"log_file,omitempty"`
	Endpoint      string `yaml:"endpoint,omitempty"`
	APIKey        string `yaml:"api_key,omitempty"`
	DBPath        string `yaml:"db_path,omitempty"`        // SQLite database path
	BufferSize    int    `yaml:"buffer_size,omitempty"`     // max events before flush (default: 50)
	FlushInterval string `yaml:"flush_interval,omitempty"` // e.g. "30s" (default: 30s)
	Retention     string `yaml:"retention,omitempty"`       // e.g. "720h" for 30 days
	TrackAll      bool   `yaml:"track_all,omitempty"`      // track non-agent requests too
}

// SecurityConfig controls CORS, security headers, and robots.txt.
type SecurityConfig struct {
	Enabled     bool     `yaml:"enabled"`
	CORSOrigins []string `yaml:"cors_origins,omitempty"`
	CORSMethods []string `yaml:"cors_methods,omitempty"`
	CORSHeaders []string `yaml:"cors_headers,omitempty"`
	CORSCredentials bool `yaml:"cors_credentials,omitempty"`
	CORSMaxAge  int      `yaml:"cors_max_age,omitempty"`

	// Security headers
	HSTSMaxAge            int    `yaml:"hsts_max_age,omitempty"`
	HSTSIncludeSubdomains *bool  `yaml:"hsts_include_subdomains,omitempty"`
	FrameOptions          string `yaml:"frame_options,omitempty"`   // DENY, SAMEORIGIN, or "" to disable
	ContentTypeOptions    string `yaml:"content_type_options,omitempty"` // nosniff or "" to disable
	ReferrerPolicy        string `yaml:"referrer_policy,omitempty"`
	CSP                   string `yaml:"csp,omitempty"`
	PermissionsPolicy     string `yaml:"permissions_policy,omitempty"`

	// robots.txt
	RobotsTxt *RobotsTxtConfig `yaml:"robots_txt,omitempty"`
}

// RobotsTxtConfig controls robots.txt generation.
type RobotsTxtConfig struct {
	Rules          []RobotsTxtRule `yaml:"rules,omitempty"`
	Sitemaps       []string        `yaml:"sitemaps,omitempty"`
	IncludeAIAgents *bool          `yaml:"include_ai_agents,omitempty"`
	AIAgentPolicy  string          `yaml:"ai_agent_policy,omitempty"` // allow or disallow
	AIAllow        []string        `yaml:"ai_allow,omitempty"`
	AIDisallow     []string        `yaml:"ai_disallow,omitempty"`
}

// RobotsTxtRule defines a single robots.txt rule block.
type RobotsTxtRule struct {
	UserAgent  string   `yaml:"user_agent"`
	Allow      []string `yaml:"allow,omitempty"`
	Disallow   []string `yaml:"disallow,omitempty"`
	CrawlDelay int      `yaml:"crawl_delay,omitempty"`
}

// AdminConfig controls the admin API server.
type AdminConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Port      int    `yaml:"port"`
	AuthToken string `yaml:"auth_token,omitempty"`
}

// AgentsTxtConfig controls per-agent access rules via agents.txt.
type AgentsTxtConfig struct {
	Enabled      bool              `yaml:"enabled"`
	Rules        []AgentsTxtRule   `yaml:"rules,omitempty"`
	SiteName     string            `yaml:"site_name,omitempty"`
	Contact      string            `yaml:"contact,omitempty"`
	DiscoveryURL string            `yaml:"discovery_url,omitempty"`
}

// AgentsTxtRule defines access rules for a specific agent pattern.
type AgentsTxtRule struct {
	Agent              string             `yaml:"agent"`
	Allow              []string           `yaml:"allow,omitempty"`
	Deny               []string           `yaml:"deny,omitempty"`
	RateLimit          *AgentsTxtRateLimit `yaml:"rate_limit,omitempty"`
	PreferredInterface string             `yaml:"preferred_interface,omitempty"` // rest, mcp, graphql, a2a
	Auth               *AgentsTxtAuth     `yaml:"auth,omitempty"`
	Description        string             `yaml:"description,omitempty"`
}

// AgentsTxtRateLimit declares a rate limit in agents.txt.
type AgentsTxtRateLimit struct {
	Max           int `yaml:"max"`
	WindowSeconds int `yaml:"window_seconds,omitempty"` // default: 60
}

// AgentsTxtAuth declares auth requirements in agents.txt.
type AgentsTxtAuth struct {
	Type     string `yaml:"type"`               // bearer, api_key, oauth2, none
	Endpoint string `yaml:"endpoint,omitempty"`
	DocsURL  string `yaml:"docs_url,omitempty"`
}

// OAuth2Config controls the OAuth2 authorization server plugin.
type OAuth2Config struct {
	Enabled               bool              `yaml:"enabled"`
	Issuer                string            `yaml:"issuer,omitempty"`
	ClientID              string            `yaml:"client_id,omitempty"`
	ClientSecret          string            `yaml:"client_secret,omitempty"`
	AuthorizationEndpoint string            `yaml:"authorization_endpoint,omitempty"`
	TokenEndpoint         string            `yaml:"token_endpoint,omitempty"`
	RedirectURI           string            `yaml:"redirect_uri,omitempty"`
	Scopes                map[string]string `yaml:"scopes,omitempty"`
	TokenTTL              int               `yaml:"token_ttl,omitempty"`       // seconds, default 3600
	RefreshTokenTTL       int               `yaml:"refresh_token_ttl,omitempty"` // seconds, default 86400
	CodeTTL               int               `yaml:"code_ttl,omitempty"`        // seconds, default 600
	Audience              string            `yaml:"audience,omitempty"`
}

// MCPConfig controls the MCP JSON-RPC server plugin.
type MCPConfig struct {
	Enabled      bool             `yaml:"enabled"`
	Endpoint     string           `yaml:"endpoint,omitempty"` // default: /mcp
	Name         string           `yaml:"name,omitempty"`
	Version      string           `yaml:"version,omitempty"`
	Instructions string           `yaml:"instructions,omitempty"`
	Tools        []MCPToolConfig  `yaml:"tools,omitempty"` // manual tool definitions
}

// MCPToolConfig defines a manually configured MCP tool.
type MCPToolConfig struct {
	Name        string                 `yaml:"name"`
	Description string                 `yaml:"description"`
	InputSchema map[string]interface{} `yaml:"input_schema,omitempty"`
}

// APIKeysConfig controls the scoped API key authentication plugin.
type APIKeysConfig struct {
	Enabled    bool             `yaml:"enabled"`
	Store      string           `yaml:"store,omitempty"` // "memory" or "sqlite", default: "memory"
	Prefix     string           `yaml:"prefix,omitempty"` // key prefix, default: "llgw_"
	AdminPath  string           `yaml:"admin_path,omitempty"` // admin API path, default: "/api/keys"
	Keys       []APIKeyConfig   `yaml:"keys,omitempty"` // pre-configured keys
}

// APIKeyConfig defines a pre-configured API key.
type APIKeyConfig struct {
	ID        string                 `yaml:"id"`
	CompanyID string                 `yaml:"company_id,omitempty"`
	UserID    string                 `yaml:"user_id,omitempty"`
	Scopes    []string               `yaml:"scopes"`
	ExpiresAt string                 `yaml:"expires_at,omitempty"` // RFC 3339
	Metadata  map[string]interface{} `yaml:"metadata,omitempty"`
}

// A2AConfig controls the A2A protocol server plugin.
type A2AConfig struct {
	Enabled           bool   `yaml:"enabled"`
	Endpoint          string `yaml:"endpoint,omitempty"`           // default: /a2a
	Streaming         bool   `yaml:"streaming,omitempty"`          // enable SSE streaming
	PushNotifications bool   `yaml:"push_notifications,omitempty"` // enable webhook push
	PushURL           string `yaml:"push_url,omitempty"`           // default push URL
	TaskTTL           string `yaml:"task_ttl,omitempty"`           // completed task retention, default: 24h
	MaxTasks          int    `yaml:"max_tasks,omitempty"`          // max concurrent tasks, default: 10000
	DBPath            string `yaml:"db_path,omitempty"`            // SQLite path for task persistence
}

// AgUIConfig controls the AG-UI SSE streaming plugin.
type AgUIConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Endpoint string `yaml:"endpoint,omitempty"` // default: /ag-ui
}

// AgentOnboardingConfig controls the agent self-registration plugin.
type AgentOnboardingConfig struct {
	Enabled             bool                        `yaml:"enabled"`
	ProvisioningWebhook string                      `yaml:"provisioning_webhook"`
	WebhookSecret       string                      `yaml:"webhook_secret,omitempty"`
	WebhookTimeout      string                      `yaml:"webhook_timeout,omitempty"` // duration string, default: 10s
	RequireIdentity     bool                        `yaml:"require_identity,omitempty"`
	AllowedProviders    []string                    `yaml:"allowed_providers,omitempty"`
	AuthDocs            string                      `yaml:"auth_docs,omitempty"`
	RateLimit           *AgentOnboardingRateLimitCfg `yaml:"rate_limit,omitempty"`
}

// AgentOnboardingRateLimitCfg controls registration rate limiting.
type AgentOnboardingRateLimitCfg struct {
	MaxRegistrations int    `yaml:"max_registrations,omitempty"` // per IP per window, default: 10
	Window           string `yaml:"window,omitempty"`            // duration string, default: 1h
}

// LoadConfig reads a YAML config file, applies defaults, applies environment
// variable overrides, and validates the result.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	ApplyDefaults(cfg)
	ApplyEnvOverrides(cfg)

	if err := Validate(cfg); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// Load reads a YAML config file and returns a parsed Config (no defaults/validation).
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse unmarshals YAML bytes into a Config.
func Parse(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	return &cfg, nil
}

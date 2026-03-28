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
	Payments        PaymentsConfig        `yaml:"payments"`
	Analytics       AnalyticsConfig       `yaml:"analytics"`
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

// AdminConfig controls the admin API server.
type AdminConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Port      int    `yaml:"port"`
	AuthToken string `yaml:"auth_token,omitempty"`
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

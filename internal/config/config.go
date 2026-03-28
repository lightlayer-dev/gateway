package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

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
	URL     string        `yaml:"url"`
	Timeout time.Duration `yaml:"timeout"`
	Retries int           `yaml:"retries,omitempty"`
}

// PluginsConfig groups all plugin configurations.
type PluginsConfig struct {
	Discovery  DiscoveryConfig  `yaml:"discovery"`
	Identity   IdentityConfig   `yaml:"identity"`
	Payments   PaymentsConfig   `yaml:"payments"`
	RateLimits RateLimitsConfig `yaml:"rate_limits"`
	Analytics  AnalyticsConfig  `yaml:"analytics"`
	Security   SecurityConfig   `yaml:"security"`
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
	Enabled        bool     `yaml:"enabled"`
	Mode           string   `yaml:"mode"` // log, warn, enforce
	TrustedIssuers []string `yaml:"trusted_issuers,omitempty"`
}

// PaymentsConfig controls x402 payment handling.
type PaymentsConfig struct {
	Enabled      bool           `yaml:"enabled"`
	Facilitator  string         `yaml:"facilitator,omitempty"`
	Routes       []PaymentRoute `yaml:"routes,omitempty"`
}

// PaymentRoute defines pricing for a specific path pattern.
type PaymentRoute struct {
	Path     string `yaml:"path"`
	Price    string `yaml:"price"`
	Currency string `yaml:"currency"`
}

// RateLimitsConfig controls per-agent rate limiting.
type RateLimitsConfig struct {
	Enabled  bool                 `yaml:"enabled"`
	Default  RateLimit            `yaml:"default"`
	PerAgent map[string]RateLimit `yaml:"per_agent,omitempty"`
}

// RateLimit defines a request count within a time window.
type RateLimit struct {
	Requests int           `yaml:"requests"`
	Window   time.Duration `yaml:"window"`
}

// AnalyticsConfig controls traffic logging and reporting.
type AnalyticsConfig struct {
	Enabled  bool   `yaml:"enabled"`
	LogFile  string `yaml:"log_file,omitempty"`
	Endpoint string `yaml:"endpoint,omitempty"`
	APIKey   string `yaml:"api_key,omitempty"`
}

// SecurityConfig controls CORS and security headers.
type SecurityConfig struct {
	Enabled     bool     `yaml:"enabled"`
	CORSOrigins []string `yaml:"cors_origins,omitempty"`
}

// AdminConfig controls the admin API server.
type AdminConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Port      int    `yaml:"port"`
	AuthToken string `yaml:"auth_token,omitempty"`
}

// Load reads a YAML config file and returns a parsed Config.
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
		return nil, err
	}
	return &cfg, nil
}

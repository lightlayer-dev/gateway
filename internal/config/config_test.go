package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// LoadConfig (full pipeline: parse → defaults → env → validate)
// ---------------------------------------------------------------------------

func TestLoadConfigTemplateFile(t *testing.T) {
	cfg, err := LoadConfig("../../configs/gateway.yaml")
	require.NoError(t, err)

	// Gateway
	assert.Equal(t, 8080, cfg.Gateway.Listen.Port)
	assert.Equal(t, "0.0.0.0", cfg.Gateway.Listen.Host)
	assert.Equal(t, "https://api.example.com", cfg.Gateway.Origin.URL)
	assert.Equal(t, 30*time.Second, cfg.Gateway.Origin.Timeout.Duration)

	// Plugins
	assert.True(t, cfg.Plugins.Discovery.Enabled)
	assert.Equal(t, "Example API", cfg.Plugins.Discovery.Name)
	assert.Len(t, cfg.Plugins.Discovery.Capabilities, 1)
	assert.Equal(t, "widgets", cfg.Plugins.Discovery.Capabilities[0].Name)

	assert.False(t, cfg.Plugins.Payments.Enabled)

	assert.True(t, cfg.Plugins.Analytics.Enabled)
	assert.Equal(t, "./agent-traffic.log", cfg.Plugins.Analytics.LogFile)

	// Admin
	assert.True(t, cfg.Admin.Enabled)
	assert.Equal(t, 9090, cfg.Admin.Port)
}

func TestLoadConfigNonexistentFile(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reading config file")
}

func TestLoadConfigInvalidYAML(t *testing.T) {
	tmp := writeTemp(t, []byte("{{{{not yaml"))
	_, err := LoadConfig(tmp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing config file")
}

func TestLoadConfigValidationFailure(t *testing.T) {
	// Missing origin URL → validation error.
	tmp := writeTemp(t, []byte("gateway:\n  listen:\n    port: 8080\n"))
	_, err := LoadConfig(tmp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "config validation failed")
	assert.Contains(t, err.Error(), "origin.url is required")
}

// ---------------------------------------------------------------------------
// Parse (raw YAML → Config, no defaults/validation)
// ---------------------------------------------------------------------------

func TestParseMinimalConfig(t *testing.T) {
	yml := []byte(`
gateway:
  origin:
    url: http://localhost:3000
`)
	cfg, err := Parse(yml)
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:3000", cfg.Gateway.Origin.URL)
}

// ---------------------------------------------------------------------------
// Duration unmarshalling
// ---------------------------------------------------------------------------

func TestDurationParsing(t *testing.T) {
	tests := []struct {
		yaml     string
		expected time.Duration
	}{
		{"timeout: 30s", 30 * time.Second},
		{"timeout: 1m", time.Minute},
		{"timeout: 1h", time.Hour},
		{"timeout: 500ms", 500 * time.Millisecond},
		{"timeout: 1m30s", 90 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.yaml, func(t *testing.T) {
			yml := []byte("gateway:\n  origin:\n    url: http://x.com\n    " + tt.yaml)
			cfg, err := Parse(yml)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, cfg.Gateway.Origin.Timeout.Duration)
		})
	}
}

func TestDurationInvalidString(t *testing.T) {
	yml := []byte("gateway:\n  origin:\n    url: http://x.com\n    timeout: not-a-duration")
	_, err := Parse(yml)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid duration")
}

func TestDurationNumericSeconds(t *testing.T) {
	yml := []byte("gateway:\n  origin:\n    url: http://x.com\n    timeout: 60")
	cfg, err := Parse(yml)
	require.NoError(t, err)
	assert.Equal(t, 60*time.Second, cfg.Gateway.Origin.Timeout.Duration)
}

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

func TestApplyDefaults(t *testing.T) {
	cfg := &Config{}
	ApplyDefaults(cfg)

	assert.Equal(t, 8080, cfg.Gateway.Listen.Port)
	assert.Equal(t, "0.0.0.0", cfg.Gateway.Listen.Host)
	assert.Equal(t, 30*time.Second, cfg.Gateway.Origin.Timeout.Duration)
	assert.Equal(t, 9090, cfg.Admin.Port)
}

func TestDefaultsDoNotOverrideExplicit(t *testing.T) {
	cfg := &Config{}
	cfg.Gateway.Listen.Port = 3000
	cfg.Gateway.Listen.Host = "127.0.0.1"
	cfg.Admin.Port = 8888

	ApplyDefaults(cfg)

	assert.Equal(t, 3000, cfg.Gateway.Listen.Port)
	assert.Equal(t, "127.0.0.1", cfg.Gateway.Listen.Host)
	assert.Equal(t, 8888, cfg.Admin.Port)
}

// ---------------------------------------------------------------------------
// Environment variable overrides
// ---------------------------------------------------------------------------

func TestEnvOverrides(t *testing.T) {
	cfg := &Config{}
	ApplyDefaults(cfg)

	t.Setenv("LIGHTLAYER_PORT", "3000")
	t.Setenv("LIGHTLAYER_HOST", "127.0.0.1")
	t.Setenv("LIGHTLAYER_ORIGIN_URL", "http://my-api:5000")
	t.Setenv("LIGHTLAYER_ADMIN_PORT", "7070")

	ApplyEnvOverrides(cfg)

	assert.Equal(t, 3000, cfg.Gateway.Listen.Port)
	assert.Equal(t, "127.0.0.1", cfg.Gateway.Listen.Host)
	assert.Equal(t, "http://my-api:5000", cfg.Gateway.Origin.URL)
	assert.Equal(t, 7070, cfg.Admin.Port)
}

func TestEnvOverridesInvalidPortIgnored(t *testing.T) {
	cfg := &Config{}
	ApplyDefaults(cfg)

	t.Setenv("LIGHTLAYER_PORT", "not-a-number")
	ApplyEnvOverrides(cfg)

	assert.Equal(t, 8080, cfg.Gateway.Listen.Port) // unchanged
}

func TestEnvOverridesPartial(t *testing.T) {
	cfg := &Config{}
	ApplyDefaults(cfg)

	t.Setenv("LIGHTLAYER_ORIGIN_URL", "http://backend:9000")
	ApplyEnvOverrides(cfg)

	assert.Equal(t, "http://backend:9000", cfg.Gateway.Origin.URL)
	assert.Equal(t, 8080, cfg.Gateway.Listen.Port) // default preserved
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

func TestValidateValidConfig(t *testing.T) {
	cfg := validConfig()
	assert.NoError(t, Validate(cfg))
}

func TestValidateOriginRequired(t *testing.T) {
	cfg := validConfig()
	cfg.Gateway.Origin.URL = ""
	err := Validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "origin.url is required")
}

func TestValidateOriginMustBeURL(t *testing.T) {
	cfg := validConfig()
	cfg.Gateway.Origin.URL = "not a url"
	err := Validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a valid URL")
}

func TestValidatePortRange(t *testing.T) {
	cfg := validConfig()
	cfg.Gateway.Listen.Port = 0
	err := Validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "listen.port")

	cfg = validConfig()
	cfg.Gateway.Listen.Port = 70000
	err = Validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "listen.port")

	cfg = validConfig()
	cfg.Admin.Port = -1
	err = Validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "admin.port")
}

func TestValidatePaymentRoutes(t *testing.T) {
	cfg := validConfig()
	cfg.Plugins.Payments.Enabled = true
	cfg.Plugins.Payments.Routes = []PaymentRoute{
		{Path: "", Price: "0.01", Currency: "USDC"},
	}
	err := Validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "path is required")

	cfg.Plugins.Payments.Routes = []PaymentRoute{
		{Path: "/api/v1", Price: "", Currency: "USDC"},
	}
	err = Validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "price is required")

	cfg.Plugins.Payments.Routes = []PaymentRoute{
		{Path: "/api/v1", Price: "0.01", Currency: ""},
	}
	err = Validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "currency is required")
}

func TestValidateMultipleErrors(t *testing.T) {
	cfg := &Config{}
	// Port 0 + no origin URL → at least 2 errors.
	err := Validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "origin.url is required")
	assert.Contains(t, err.Error(), "listen.port")
}

// ---------------------------------------------------------------------------
// Full pipeline integration
// ---------------------------------------------------------------------------

func TestLoadConfigWithEnvOverride(t *testing.T) {
	yml := []byte(`
gateway:
  origin:
    url: http://original:3000
`)
	tmp := writeTemp(t, yml)

	t.Setenv("LIGHTLAYER_ORIGIN_URL", "http://overridden:5000")
	t.Setenv("LIGHTLAYER_PORT", "4000")

	cfg, err := LoadConfig(tmp)
	require.NoError(t, err)

	assert.Equal(t, "http://overridden:5000", cfg.Gateway.Origin.URL)
	assert.Equal(t, 4000, cfg.Gateway.Listen.Port)
	// Defaults still applied for unset fields.
	assert.Equal(t, "0.0.0.0", cfg.Gateway.Listen.Host)
	assert.Equal(t, 9090, cfg.Admin.Port)
}

func TestLoadConfigDefaultsApplied(t *testing.T) {
	yml := []byte(`
gateway:
  origin:
    url: http://backend:3000
`)
	tmp := writeTemp(t, yml)
	cfg, err := LoadConfig(tmp)
	require.NoError(t, err)

	assert.Equal(t, 8080, cfg.Gateway.Listen.Port)
	assert.Equal(t, "0.0.0.0", cfg.Gateway.Listen.Host)
	assert.Equal(t, 30*time.Second, cfg.Gateway.Origin.Timeout.Duration)
	assert.Equal(t, 9090, cfg.Admin.Port)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeTemp(t *testing.T, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "gateway.yaml")
	require.NoError(t, os.WriteFile(p, data, 0644))
	return p
}

func validConfig() *Config {
	cfg := &Config{}
	cfg.Gateway.Origin.URL = "https://api.example.com"
	cfg.Gateway.Listen.Port = 8080
	cfg.Admin.Port = 9090
	return cfg
}

package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadTemplateConfig(t *testing.T) {
	cfg, err := Load("../../configs/gateway.yaml")
	require.NoError(t, err)

	// Gateway
	assert.Equal(t, 8080, cfg.Gateway.Listen.Port)
	assert.Equal(t, "0.0.0.0", cfg.Gateway.Listen.Host)
	assert.Equal(t, "https://api.example.com", cfg.Gateway.Origin.URL)
	assert.Equal(t, 30*time.Second, cfg.Gateway.Origin.Timeout)

	// Plugins
	assert.True(t, cfg.Plugins.Discovery.Enabled)
	assert.Equal(t, "Example API", cfg.Plugins.Discovery.Name)
	assert.Len(t, cfg.Plugins.Discovery.Capabilities, 1)
	assert.Equal(t, "widgets", cfg.Plugins.Discovery.Capabilities[0].Name)

	assert.True(t, cfg.Plugins.Identity.Enabled)
	assert.Equal(t, "enforce", cfg.Plugins.Identity.Mode)

	assert.False(t, cfg.Plugins.Payments.Enabled)

	assert.True(t, cfg.Plugins.RateLimits.Enabled)
	assert.Equal(t, 100, cfg.Plugins.RateLimits.Default.Requests)
	assert.Equal(t, time.Minute, cfg.Plugins.RateLimits.Default.Window)

	assert.True(t, cfg.Plugins.Analytics.Enabled)
	assert.Equal(t, "./agent-traffic.log", cfg.Plugins.Analytics.LogFile)

	assert.True(t, cfg.Plugins.Security.Enabled)

	// Admin
	assert.True(t, cfg.Admin.Enabled)
	assert.Equal(t, 9090, cfg.Admin.Port)
}

func TestParseMinimalConfig(t *testing.T) {
	yaml := []byte(`
gateway:
  origin:
    url: http://localhost:3000
`)
	cfg, err := Parse(yaml)
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:3000", cfg.Gateway.Origin.URL)
}

func TestLoadNonexistentFile(t *testing.T) {
	_, err := Load("/nonexistent/path.yaml")
	assert.True(t, os.IsNotExist(err))
}

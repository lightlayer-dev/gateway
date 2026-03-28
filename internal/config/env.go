package config

import (
	"os"
	"strconv"
	"time"
)

// ApplyEnvOverrides reads environment variables and overrides the corresponding
// config fields. Called after YAML loading, before validation.
//
// Every config field that's commonly set in Docker/cloud deployments has an
// env var equivalent. This makes it easy to configure the gateway without
// mounting a YAML file — just pass env vars.
func ApplyEnvOverrides(cfg *Config) {
	// ── Gateway ─────────────────────────────────────────────────────────

	// Listen
	envInt("LIGHTLAYER_PORT", &cfg.Gateway.Listen.Port)
	envStr("LIGHTLAYER_HOST", &cfg.Gateway.Listen.Host)
	envStr("LIGHTLAYER_TLS_CERT", &cfg.Gateway.Listen.TLS.Cert)
	envStr("LIGHTLAYER_TLS_KEY", &cfg.Gateway.Listen.TLS.Key)

	// Origin
	envStr("LIGHTLAYER_ORIGIN_URL", &cfg.Gateway.Origin.URL)
	envDuration("LIGHTLAYER_ORIGIN_TIMEOUT", &cfg.Gateway.Origin.Timeout)
	envInt("LIGHTLAYER_ORIGIN_RETRIES", &cfg.Gateway.Origin.Retries)
	envBool("LIGHTLAYER_ORIGIN_TLS_SKIP_VERIFY", &cfg.Gateway.Origin.TLSSkipVerify)

	// ── Admin ───────────────────────────────────────────────────────────

	envBool("LIGHTLAYER_ADMIN_ENABLED", &cfg.Admin.Enabled)
	envInt("LIGHTLAYER_ADMIN_PORT", &cfg.Admin.Port)
	envStr("LIGHTLAYER_ADMIN_AUTH_TOKEN", &cfg.Admin.AuthToken)

	// ── Plugins (enable/disable) ────────────────────────────────────────

	envBool("LIGHTLAYER_DISCOVERY_ENABLED", &cfg.Plugins.Discovery.Enabled)
	envBool("LIGHTLAYER_IDENTITY_ENABLED", &cfg.Plugins.Identity.Enabled)
	envStr("LIGHTLAYER_IDENTITY_MODE", &cfg.Plugins.Identity.Mode)
	envBool("LIGHTLAYER_RATELIMIT_ENABLED", &cfg.Plugins.RateLimits.Enabled)
	envBool("LIGHTLAYER_PAYMENTS_ENABLED", &cfg.Plugins.Payments.Enabled)
	envBool("LIGHTLAYER_ANALYTICS_ENABLED", &cfg.Plugins.Analytics.Enabled)
	envStr("LIGHTLAYER_ANALYTICS_LOG_FILE", &cfg.Plugins.Analytics.LogFile)
	envBool("LIGHTLAYER_SECURITY_ENABLED", &cfg.Plugins.Security.Enabled)
	envBool("LIGHTLAYER_MCP_ENABLED", &cfg.Plugins.MCP.Enabled)
	envBool("LIGHTLAYER_A2A_ENABLED", &cfg.Plugins.A2A.Enabled)
	envBool("LIGHTLAYER_AGUI_ENABLED", &cfg.Plugins.AgUI.Enabled)
	envBool("LIGHTLAYER_APIKEYS_ENABLED", &cfg.Plugins.APIKeys.Enabled)
	envBool("LIGHTLAYER_AGENTSTXT_ENABLED", &cfg.Plugins.AgentsTxt.Enabled)
	envBool("LIGHTLAYER_OAUTH2_ENABLED", &cfg.Plugins.OAuth2.Enabled)
}

// ── Helpers ──────────────────────────────────────────────────────────────

func envStr(key string, target *string) {
	if v := os.Getenv(key); v != "" {
		*target = v
	}
}

func envInt(key string, target *int) {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			*target = n
		}
	}
}

func envBool(key string, target *bool) {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			*target = b
		}
	}
}

func envDuration(key string, target *Duration) {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			target.Duration = d
		}
	}
}

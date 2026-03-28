package config

import (
	"os"
	"strconv"
)

// ApplyEnvOverrides applies the small set of environment variables that
// need to work *before* or *instead of* a config file — bootstrap-level
// settings only. Everything else belongs in gateway.yaml.
//
// Design: follows Caddy/Cloudflare convention — config file is the source
// of truth, env vars are only for the things that locate or bootstrap it.
func ApplyEnvOverrides(cfg *Config) {
	// Where to listen — needed to override in Docker without a config mount.
	envInt("LIGHTLAYER_PORT", &cfg.Gateway.Listen.Port)
	envStr("LIGHTLAYER_HOST", &cfg.Gateway.Listen.Host)

	// Where to proxy — the single most common override.
	envStr("LIGHTLAYER_ORIGIN_URL", &cfg.Gateway.Origin.URL)

	// Admin port — occasionally different per environment.
	envInt("LIGHTLAYER_ADMIN_PORT", &cfg.Admin.Port)
}

// Note: LIGHTLAYER_CONFIG (config file path) is handled in the CLI loader,
// not here, since it's needed before config is parsed.

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

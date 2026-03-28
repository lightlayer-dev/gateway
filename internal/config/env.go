package config

import (
	"os"
	"strconv"
)

// ApplyEnvOverrides reads environment variables and overrides the corresponding
// config fields. Called after YAML loading, before validation.
func ApplyEnvOverrides(cfg *Config) {
	if v := os.Getenv("LIGHTLAYER_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.Gateway.Listen.Port = port
		}
	}

	if v := os.Getenv("LIGHTLAYER_HOST"); v != "" {
		cfg.Gateway.Listen.Host = v
	}

	if v := os.Getenv("LIGHTLAYER_ORIGIN_URL"); v != "" {
		cfg.Gateway.Origin.URL = v
	}

	if v := os.Getenv("LIGHTLAYER_ADMIN_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.Admin.Port = port
		}
	}
}

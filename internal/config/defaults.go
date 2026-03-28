package config

import "time"

// ApplyDefaults fills in zero-valued fields with sensible defaults.
func ApplyDefaults(cfg *Config) {
	// Gateway defaults
	if cfg.Gateway.Listen.Port == 0 {
		cfg.Gateway.Listen.Port = 8080
	}
	if cfg.Gateway.Listen.Host == "" {
		cfg.Gateway.Listen.Host = "0.0.0.0"
	}
	if cfg.Gateway.Origin.Timeout.Duration == 0 {
		cfg.Gateway.Origin.Timeout.Duration = 30 * time.Second
	}

	// Rate limits defaults
	if cfg.Plugins.RateLimits.Default.Requests == 0 {
		cfg.Plugins.RateLimits.Default.Requests = 100
	}
	if cfg.Plugins.RateLimits.Default.Window.Duration == 0 {
		cfg.Plugins.RateLimits.Default.Window.Duration = time.Minute
	}

	// Admin defaults
	if cfg.Admin.Port == 0 {
		cfg.Admin.Port = 9090
	}

	// Analytics enabled by default (log to stdout via empty log_file)
	// All other plugins disabled by default — their Enabled field zero-value is false.
}

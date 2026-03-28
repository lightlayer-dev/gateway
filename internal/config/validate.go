package config

import (
	"errors"
	"fmt"
	"net/url"
)

// Validate checks the config for correctness. Returns a combined error
// describing all problems found.
func Validate(cfg *Config) error {
	var errs []error

	// Origin URL is required and must be valid.
	if cfg.Gateway.Origin.URL == "" {
		errs = append(errs, errors.New("gateway.origin.url is required"))
	} else if u, err := url.Parse(cfg.Gateway.Origin.URL); err != nil || u.Scheme == "" || u.Host == "" {
		errs = append(errs, fmt.Errorf("gateway.origin.url %q is not a valid URL (need scheme and host)", cfg.Gateway.Origin.URL))
	}

	// Port numbers must be in range.
	if err := validatePort("gateway.listen.port", cfg.Gateway.Listen.Port); err != nil {
		errs = append(errs, err)
	}
	if err := validatePort("admin.port", cfg.Admin.Port); err != nil {
		errs = append(errs, err)
	}

	// Rate limit window must be positive when rate limits are enabled.
	if cfg.Plugins.RateLimits.Enabled {
		if cfg.Plugins.RateLimits.Default.Window.Duration <= 0 {
			errs = append(errs, errors.New("plugins.rate_limits.default.window must be a positive duration"))
		}
		if cfg.Plugins.RateLimits.Default.Requests <= 0 {
			errs = append(errs, errors.New("plugins.rate_limits.default.requests must be positive"))
		}
	}

	// Payment routes must have required fields when payments are enabled.
	if cfg.Plugins.Payments.Enabled {
		for i, r := range cfg.Plugins.Payments.Routes {
			if r.Path == "" {
				errs = append(errs, fmt.Errorf("plugins.payments.routes[%d].path is required", i))
			}
			if r.Price == "" {
				errs = append(errs, fmt.Errorf("plugins.payments.routes[%d].price is required", i))
			}
			if r.Currency == "" {
				errs = append(errs, fmt.Errorf("plugins.payments.routes[%d].currency is required", i))
			}
		}
	}

	return errors.Join(errs...)
}

func validatePort(field string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s %d is out of range (must be 1-65535)", field, port)
	}
	return nil
}

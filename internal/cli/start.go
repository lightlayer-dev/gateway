package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lightlayer-dev/gateway/internal/config"
	"github.com/lightlayer-dev/gateway/internal/plugins"
	"github.com/lightlayer-dev/gateway/internal/proxy"
	"github.com/spf13/cobra"
)

var startConfigPath string

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the gateway proxy server",
	RunE:  runStart,
}

func init() {
	startCmd.Flags().StringVarP(&startConfigPath, "config", "c", "gateway.yaml", "path to config file")
	rootCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	return startServer(cmd, startConfigPath, false)
}

func startServer(cmd *cobra.Command, cfgPath string, verbose bool) error {
	if verbose {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
	}

	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		return err
	}

	p, err := proxy.NewProxy(cfg)
	if err != nil {
		return fmt.Errorf("creating proxy: %w", err)
	}

	// Build the plugin pipeline from config and wrap the proxy.
	pipeline, err := plugins.BuildPipeline(pluginConfigs(cfg))
	if err != nil {
		return fmt.Errorf("building plugin pipeline: %w", err)
	}
	handler := pipeline.Wrap(p)

	printBanner(cmd, cfg)

	addr := fmt.Sprintf("%s:%d", cfg.Gateway.Listen.Host, cfg.Gateway.Listen.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	// Admin server.
	var adminSrv *http.Server
	if cfg.Admin.Enabled {
		adminAddr := fmt.Sprintf(":%d", cfg.Admin.Port)
		adminSrv = &http.Server{
			Addr:    adminAddr,
			Handler: adminHandler(),
		}
		go func() {
			slog.Info("admin listening", "addr", adminAddr)
			if err := adminSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("admin server error", "error", err)
			}
		}()
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("proxy listening", "addr", addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutting down gracefully...")

		// 30-second timeout for in-flight requests.
		shutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Shut down both servers concurrently.
		var shutdownErr error
		if adminSrv != nil {
			if err := adminSrv.Shutdown(shutCtx); err != nil {
				slog.Error("admin shutdown error", "error", err)
				shutdownErr = err
			}
		}
		if err := srv.Shutdown(shutCtx); err != nil {
			slog.Error("proxy shutdown error", "error", err)
			shutdownErr = err
		}

		// Close plugin pipeline.
		if err := pipeline.Close(); err != nil {
			slog.Error("plugin pipeline close error", "error", err)
		}

		slog.Info("shutdown complete")
		return shutdownErr
	case err := <-errCh:
		// If the proxy server fails, clean up admin too.
		if adminSrv != nil {
			shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			adminSrv.Shutdown(shutCtx)
		}
		pipeline.Close()
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}
}

// pluginConfigs converts gateway config into ordered PluginConfig entries.
// Plugin execution order follows the design doc.
func pluginConfigs(cfg *config.Config) []plugins.PluginConfig {
	return []plugins.PluginConfig{
		{Name: "security", Enabled: cfg.Plugins.Security.Enabled},
		{Name: "discovery", Enabled: cfg.Plugins.Discovery.Enabled},
		{Name: "identity", Enabled: cfg.Plugins.Identity.Enabled, Config: identityConfigMap(cfg)},
		{Name: "rate_limits", Enabled: cfg.Plugins.RateLimits.Enabled},
		{Name: "payments", Enabled: cfg.Plugins.Payments.Enabled},
		{Name: "analytics", Enabled: cfg.Plugins.Analytics.Enabled},
	}
}

// adminHandler returns a basic admin HTTP handler with health endpoint.
func adminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	})
	return mux
}

func printBanner(cmd *cobra.Command, cfg *config.Config) {
	w := cmd.OutOrStdout()

	fmt.Fprintf(w, "\n ⚡ LightLayer Gateway %s\n\n", Version)
	fmt.Fprintf(w, "  Listening:  http://%s:%d\n", cfg.Gateway.Listen.Host, cfg.Gateway.Listen.Port)
	fmt.Fprintf(w, "  Origin:     %s\n", cfg.Gateway.Origin.URL)
	if cfg.Admin.Enabled {
		fmt.Fprintf(w, "  Admin:      http://localhost:%d\n", cfg.Admin.Port)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  Plugins:")

	type pluginInfo struct {
		name    string
		enabled bool
		detail  string
	}

	plugins := []pluginInfo{
		{"discovery", cfg.Plugins.Discovery.Enabled, "serving /.well-known/ai, /agents.txt, /llms.txt"},
		{"identity", cfg.Plugins.Identity.Enabled, fmt.Sprintf("%s mode", cfg.Plugins.Identity.Mode)},
		{"rate_limits", cfg.Plugins.RateLimits.Enabled, fmt.Sprintf("%d req/%s default", cfg.Plugins.RateLimits.Default.Requests, cfg.Plugins.RateLimits.Default.Window.Duration)},
		{"analytics", cfg.Plugins.Analytics.Enabled, analyticsDetail(cfg)},
		{"security", cfg.Plugins.Security.Enabled, "CORS + security headers"},
		{"payments", cfg.Plugins.Payments.Enabled, "x402 payment handling"},
	}

	for _, p := range plugins {
		if p.enabled {
			fmt.Fprintf(w, "    ✓ %-14s %s\n", p.name, p.detail)
		} else {
			fmt.Fprintf(w, "    ✗ %-14s disabled\n", p.name)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "  Ready to proxy agent traffic.")
	fmt.Fprintln(w)
}

// identityConfigMap converts IdentityConfig into a generic map for the plugin.
func identityConfigMap(cfg *config.Config) map[string]interface{} {
	ic := cfg.Plugins.Identity
	m := map[string]interface{}{
		"mode":           ic.Mode,
		"trusted_issuers": ic.TrustedIssuers,
	}
	if len(ic.Audience) > 0 {
		m["audience"] = ic.Audience
	}
	if len(ic.TrustedDomains) > 0 {
		m["trusted_domains"] = ic.TrustedDomains
	}
	if ic.DefaultPolicy != "" {
		m["default_policy"] = ic.DefaultPolicy
	}
	if ic.HeaderName != "" {
		m["header_name"] = ic.HeaderName
	}
	if ic.TokenPrefix != "" {
		m["token_prefix"] = ic.TokenPrefix
	}
	if ic.ClockSkewSeconds != 0 {
		m["clock_skew_seconds"] = ic.ClockSkewSeconds
	}
	if ic.MaxLifetimeSeconds != 0 {
		m["max_lifetime_seconds"] = ic.MaxLifetimeSeconds
	}
	if len(ic.Policies) > 0 {
		policies := make([]map[string]interface{}, len(ic.Policies))
		for i, p := range ic.Policies {
			pol := map[string]interface{}{"name": p.Name}
			if p.AgentPattern != "" {
				pol["agent_pattern"] = p.AgentPattern
			}
			if len(p.TrustDomains) > 0 {
				pol["trust_domains"] = p.TrustDomains
			}
			if len(p.RequiredScopes) > 0 {
				pol["required_scopes"] = p.RequiredScopes
			}
			if len(p.Methods) > 0 {
				pol["methods"] = p.Methods
			}
			if len(p.Paths) > 0 {
				pol["paths"] = p.Paths
			}
			if p.AllowDelegated != nil {
				pol["allow_delegated"] = *p.AllowDelegated
			}
			policies[i] = pol
		}
		m["policies"] = policies
	}
	return m
}

func analyticsDetail(cfg *config.Config) string {
	if cfg.Plugins.Analytics.LogFile != "" {
		return fmt.Sprintf("logging to %s", cfg.Plugins.Analytics.LogFile)
	}
	if cfg.Plugins.Analytics.Endpoint != "" {
		return fmt.Sprintf("reporting to %s", cfg.Plugins.Analytics.Endpoint)
	}
	return "enabled"
}

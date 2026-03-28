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
	_ "github.com/lightlayer-dev/gateway/internal/plugins/apikeys"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/mcp"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/oauth2"
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
		{Name: "oauth2", Enabled: cfg.Plugins.OAuth2.Enabled, Config: oauth2ConfigMap(cfg)},
		{Name: "mcp", Enabled: cfg.Plugins.MCP.Enabled, Config: mcpConfigMap(cfg)},
		{Name: "agents_txt", Enabled: cfg.Plugins.AgentsTxt.Enabled},
		{Name: "api_keys", Enabled: cfg.Plugins.APIKeys.Enabled, Config: apiKeysConfigMap(cfg)},
		{Name: "identity", Enabled: cfg.Plugins.Identity.Enabled, Config: identityConfigMap(cfg)},
		{Name: "rate_limits", Enabled: cfg.Plugins.RateLimits.Enabled},
		{Name: "payments", Enabled: cfg.Plugins.Payments.Enabled, Config: paymentsConfigMap(cfg)},
		{Name: "analytics", Enabled: cfg.Plugins.Analytics.Enabled, Config: analyticsConfigMap(cfg)},
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
		{"oauth2", cfg.Plugins.OAuth2.Enabled, "PKCE flow + discovery endpoint"},
		{"mcp", cfg.Plugins.MCP.Enabled, mcpDetail(cfg)},
		{"api_keys", cfg.Plugins.APIKeys.Enabled, "scoped API key auth"},
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

// paymentsConfigMap converts PaymentsConfig into a generic map for the plugin.
func paymentsConfigMap(cfg *config.Config) map[string]interface{} {
	pc := cfg.Plugins.Payments
	m := map[string]interface{}{
		"facilitator": pc.Facilitator,
		"pay_to":      pc.PayTo,
		"network":     pc.Network,
		"scheme":      pc.Scheme,
	}
	if len(pc.Routes) > 0 {
		routes := make([]map[string]interface{}, len(pc.Routes))
		for i, r := range pc.Routes {
			route := map[string]interface{}{
				"path":  r.Path,
				"price": r.Price,
			}
			if r.Currency != "" {
				route["currency"] = r.Currency
			}
			if r.Network != "" {
				route["network"] = r.Network
			}
			if r.PayTo != "" {
				route["pay_to"] = r.PayTo
			}
			if r.Scheme != "" {
				route["scheme"] = r.Scheme
			}
			if r.MaxTimeoutSeconds != 0 {
				route["max_timeout_seconds"] = r.MaxTimeoutSeconds
			}
			if r.Description != "" {
				route["description"] = r.Description
			}
			routes[i] = route
		}
		m["routes"] = routes
	}
	return m
}

// analyticsConfigMap converts AnalyticsConfig into a generic map for the plugin.
func analyticsConfigMap(cfg *config.Config) map[string]interface{} {
	ac := cfg.Plugins.Analytics
	m := map[string]interface{}{}
	if ac.LogFile != "" {
		m["log_file"] = ac.LogFile
	}
	if ac.Endpoint != "" {
		m["endpoint"] = ac.Endpoint
	}
	if ac.APIKey != "" {
		m["api_key"] = ac.APIKey
	}
	if ac.DBPath != "" {
		m["db_path"] = ac.DBPath
	}
	if ac.BufferSize != 0 {
		m["buffer_size"] = ac.BufferSize
	}
	if ac.FlushInterval != "" {
		m["flush_interval"] = ac.FlushInterval
	}
	if ac.Retention != "" {
		m["retention"] = ac.Retention
	}
	if ac.TrackAll {
		m["track_all"] = true
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

func mcpDetail(cfg *config.Config) string {
	endpoint := cfg.Plugins.MCP.Endpoint
	if endpoint == "" {
		endpoint = "/mcp"
	}
	return fmt.Sprintf("JSON-RPC at %s", endpoint)
}

// oauth2ConfigMap converts OAuth2Config into a generic map for the plugin.
func oauth2ConfigMap(cfg *config.Config) map[string]interface{} {
	oc := cfg.Plugins.OAuth2
	m := map[string]interface{}{}
	if oc.Issuer != "" {
		m["issuer"] = oc.Issuer
	}
	if oc.ClientID != "" {
		m["client_id"] = oc.ClientID
	}
	if oc.ClientSecret != "" {
		m["client_secret"] = oc.ClientSecret
	}
	if oc.RedirectURI != "" {
		m["redirect_uri"] = oc.RedirectURI
	}
	if oc.Audience != "" {
		m["audience"] = oc.Audience
	}
	if len(oc.Scopes) > 0 {
		scopes := make(map[string]interface{}, len(oc.Scopes))
		for k, v := range oc.Scopes {
			scopes[k] = v
		}
		m["scopes"] = scopes
	}
	if oc.TokenTTL != 0 {
		m["token_ttl"] = oc.TokenTTL
	}
	if oc.RefreshTokenTTL != 0 {
		m["refresh_token_ttl"] = oc.RefreshTokenTTL
	}
	if oc.CodeTTL != 0 {
		m["code_ttl"] = oc.CodeTTL
	}
	return m
}

// mcpConfigMap converts MCPConfig into a generic map for the plugin.
func mcpConfigMap(cfg *config.Config) map[string]interface{} {
	mc := cfg.Plugins.MCP
	m := map[string]interface{}{}
	if mc.Endpoint != "" {
		m["endpoint"] = mc.Endpoint
	}
	if mc.Name != "" {
		m["name"] = mc.Name
	}
	if mc.Version != "" {
		m["version"] = mc.Version
	}
	if mc.Instructions != "" {
		m["instructions"] = mc.Instructions
	}
	if len(mc.Tools) > 0 {
		tools := make([]interface{}, len(mc.Tools))
		for i, t := range mc.Tools {
			tools[i] = map[string]interface{}{
				"name":         t.Name,
				"description":  t.Description,
				"input_schema": t.InputSchema,
			}
		}
		m["tools"] = tools
	}
	// Pass discovery capabilities for auto-generation.
	if cfg.Plugins.Discovery.Enabled && len(cfg.Plugins.Discovery.Capabilities) > 0 {
		caps := make([]interface{}, len(cfg.Plugins.Discovery.Capabilities))
		for i, c := range cfg.Plugins.Discovery.Capabilities {
			caps[i] = map[string]interface{}{
				"name":        c.Name,
				"description": c.Description,
				"methods":     c.Methods,
				"paths":       c.Paths,
			}
		}
		m["capabilities"] = caps
	}
	if cfg.Gateway.Origin.URL != "" {
		m["origin_url"] = cfg.Gateway.Origin.URL
	}
	return m
}

// apiKeysConfigMap converts APIKeysConfig into a generic map for the plugin.
func apiKeysConfigMap(cfg *config.Config) map[string]interface{} {
	ak := cfg.Plugins.APIKeys
	m := map[string]interface{}{}
	if ak.Prefix != "" {
		m["prefix"] = ak.Prefix
	}
	if ak.AdminPath != "" {
		m["admin_path"] = ak.AdminPath
	}
	if ak.Store != "" {
		m["store"] = ak.Store
	}
	if len(ak.Keys) > 0 {
		keys := make([]interface{}, len(ak.Keys))
		for i, k := range ak.Keys {
			km := map[string]interface{}{
				"id":     k.ID,
				"scopes": k.Scopes,
			}
			if k.CompanyID != "" {
				km["company_id"] = k.CompanyID
			}
			if k.UserID != "" {
				km["user_id"] = k.UserID
			}
			if k.ExpiresAt != "" {
				km["expires_at"] = k.ExpiresAt
			}
			if len(k.Metadata) > 0 {
				km["metadata"] = k.Metadata
			}
			keys[i] = km
		}
		m["keys"] = keys
	}
	return m
}

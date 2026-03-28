package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/lightlayer-dev/gateway/internal/admin"
	"github.com/lightlayer-dev/gateway/internal/config"
	"github.com/lightlayer-dev/gateway/internal/plugins"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/a2a"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/agui"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/apikeys"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/mcp"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/oauth2"
	"github.com/lightlayer-dev/gateway/internal/proxy"
	"github.com/lightlayer-dev/gateway/internal/store"
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

// gateway holds the running gateway state for hot reload.
type gateway struct {
	mu       sync.Mutex
	cmd      *cobra.Command
	cfgPath  string
	verbose  bool

	cfg      *config.Config
	pipeline *plugins.Pipeline
	proxySrv *http.Server
	adminSrv *admin.Server
	store    store.Store
	watcher  *config.Watcher

	// handler is atomically swapped on reload.
	handler atomic.Value // holds http.Handler

	reloading sync.Mutex
}

func startServer(cmd *cobra.Command, cfgPath string, verbose bool) error {
	if verbose {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
	}

	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		return err
	}

	gw := &gateway{
		cmd:     cmd,
		cfgPath: cfgPath,
		verbose: verbose,
		cfg:     cfg,
	}

	// Open analytics store if configured.
	if cfg.Plugins.Analytics.Enabled && cfg.Plugins.Analytics.DBPath != "" {
		st, err := store.NewSQLiteStore(cfg.Plugins.Analytics.DBPath)
		if err != nil {
			slog.Warn("failed to open analytics store, continuing without", "error", err)
		} else {
			gw.store = st
		}
	}

	// Build proxy and pipeline.
	p, err := proxy.NewProxy(cfg)
	if err != nil {
		return fmt.Errorf("creating proxy: %w", err)
	}

	pipeline, err := plugins.BuildPipeline(pluginConfigs(cfg))
	if err != nil {
		return fmt.Errorf("building plugin pipeline: %w", err)
	}
	gw.pipeline = pipeline

	handler := pipeline.Wrap(p)
	gw.handler.Store(handler)

	printBanner(cmd, cfg)

	// Start proxy server.
	addr := fmt.Sprintf("%s:%d", cfg.Gateway.Listen.Host, cfg.Gateway.Listen.Port)
	gw.proxySrv = &http.Server{
		Addr: addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gw.handler.Load().(http.Handler).ServeHTTP(w, r)
		}),
	}

	// Start admin server.
	if cfg.Admin.Enabled {
		gw.adminSrv = admin.NewServer(cfg, pipeline, gw.store, Version)
		gw.adminSrv.ConfigPath = cfgPath
		gw.adminSrv.ReloadFunc = gw.reload
		if err := gw.adminSrv.Start(); err != nil {
			slog.Error("admin server start failed", "error", err)
		}
	}

	// Start config file watcher for auto-reload.
	if verbose {
		watcher, err := config.NewWatcher(cfgPath, gw.reload)
		if err != nil {
			slog.Warn("config file watcher failed to start", "error", err)
		} else {
			gw.watcher = watcher
			watcher.Start()
			slog.Debug("config file watcher started", "path", cfgPath)
		}
	}

	// Set up signal handling: SIGINT/SIGTERM for shutdown, SIGHUP for reload.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sighupCh := make(chan os.Signal, 1)
	signal.Notify(sighupCh, syscall.SIGHUP)

	errCh := make(chan error, 1)
	go func() {
		slog.Info("proxy listening", "addr", addr)
		errCh <- gw.proxySrv.ListenAndServe()
	}()

	// Event loop.
	for {
		select {
		case <-sighupCh:
			slog.Info("received SIGHUP, reloading config...")
			if err := gw.reload(cfgPath); err != nil {
				slog.Error("SIGHUP reload failed", "error", err)
			}

		case <-ctx.Done():
			return gw.shutdown()

		case err := <-errCh:
			if err != nil && err != http.ErrServerClosed {
				gw.cleanup()
				return err
			}
			return nil
		}
	}
}

// reload performs a hot reload of config and plugins.
func (gw *gateway) reload(cfgPath string) error {
	gw.reloading.Lock()
	defer gw.reloading.Unlock()

	slog.Info("reloading config", "path", cfgPath)

	newCfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Build new proxy.
	p, err := proxy.NewProxy(newCfg)
	if err != nil {
		return fmt.Errorf("create proxy: %w", err)
	}

	// Build new pipeline.
	newPipeline, err := plugins.BuildPipeline(pluginConfigs(newCfg))
	if err != nil {
		return fmt.Errorf("build pipeline: %w", err)
	}

	// Atomically swap the handler.
	newHandler := newPipeline.Wrap(p)
	gw.handler.Store(newHandler)

	// Close old pipeline.
	oldPipeline := gw.pipeline
	gw.pipeline = newPipeline
	gw.cfg = newCfg

	if oldPipeline != nil {
		if err := oldPipeline.Close(); err != nil {
			slog.Warn("closing old pipeline", "error", err)
		}
	}

	// Update admin server references.
	if gw.adminSrv != nil {
		gw.adminSrv.SetConfig(newCfg)
		gw.adminSrv.SetPipeline(newPipeline)
	}

	slog.Info("config reloaded successfully")
	return nil
}

// shutdown gracefully stops all components.
func (gw *gateway) shutdown() error {
	slog.Info("shutting down gracefully...")

	shutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var shutdownErr error

	// Stop file watcher.
	if gw.watcher != nil {
		gw.watcher.Stop()
	}

	// Shutdown admin server.
	if gw.adminSrv != nil {
		if err := gw.adminSrv.Shutdown(shutCtx); err != nil {
			slog.Error("admin shutdown error", "error", err)
			shutdownErr = err
		}
	}

	// Shutdown proxy server.
	if err := gw.proxySrv.Shutdown(shutCtx); err != nil {
		slog.Error("proxy shutdown error", "error", err)
		shutdownErr = err
	}

	// Close plugins.
	if gw.pipeline != nil {
		if err := gw.pipeline.Close(); err != nil {
			slog.Error("plugin pipeline close error", "error", err)
		}
	}

	// Close store.
	if gw.store != nil {
		if err := gw.store.Close(); err != nil {
			slog.Error("store close error", "error", err)
		}
	}

	slog.Info("shutdown complete")
	return shutdownErr
}

// cleanup releases resources without graceful shutdown.
func (gw *gateway) cleanup() {
	if gw.watcher != nil {
		gw.watcher.Stop()
	}
	if gw.adminSrv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		gw.adminSrv.Shutdown(ctx)
	}
	if gw.pipeline != nil {
		gw.pipeline.Close()
	}
	if gw.store != nil {
		gw.store.Close()
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
		{Name: "a2a", Enabled: cfg.Plugins.A2A.Enabled, Config: a2aConfigMap(cfg)},
		{Name: "ag_ui", Enabled: cfg.Plugins.AgUI.Enabled, Config: agUIConfigMap(cfg)},
		{Name: "agents_txt", Enabled: cfg.Plugins.AgentsTxt.Enabled},
		{Name: "api_keys", Enabled: cfg.Plugins.APIKeys.Enabled, Config: apiKeysConfigMap(cfg)},
		{Name: "identity", Enabled: cfg.Plugins.Identity.Enabled, Config: identityConfigMap(cfg)},
		{Name: "rate_limits", Enabled: cfg.Plugins.RateLimits.Enabled},
		{Name: "payments", Enabled: cfg.Plugins.Payments.Enabled, Config: paymentsConfigMap(cfg)},
		{Name: "analytics", Enabled: cfg.Plugins.Analytics.Enabled, Config: analyticsConfigMap(cfg)},
	}
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

	pluginList := []pluginInfo{
		{"discovery", cfg.Plugins.Discovery.Enabled, "serving /.well-known/ai, /agents.txt, /llms.txt"},
		{"identity", cfg.Plugins.Identity.Enabled, fmt.Sprintf("%s mode", cfg.Plugins.Identity.Mode)},
		{"rate_limits", cfg.Plugins.RateLimits.Enabled, fmt.Sprintf("%d req/%s default", cfg.Plugins.RateLimits.Default.Requests, cfg.Plugins.RateLimits.Default.Window.Duration)},
		{"analytics", cfg.Plugins.Analytics.Enabled, analyticsDetail(cfg)},
		{"security", cfg.Plugins.Security.Enabled, "CORS + security headers"},
		{"payments", cfg.Plugins.Payments.Enabled, "x402 payment handling"},
		{"oauth2", cfg.Plugins.OAuth2.Enabled, "PKCE flow + discovery endpoint"},
		{"mcp", cfg.Plugins.MCP.Enabled, mcpDetail(cfg)},
		{"a2a", cfg.Plugins.A2A.Enabled, a2aDetail(cfg)},
		{"ag_ui", cfg.Plugins.AgUI.Enabled, agUIDetail(cfg)},
		{"api_keys", cfg.Plugins.APIKeys.Enabled, "scoped API key auth"},
	}

	for _, p := range pluginList {
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
		"mode":            ic.Mode,
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

// a2aConfigMap converts A2AConfig into a generic map for the plugin.
func a2aConfigMap(cfg *config.Config) map[string]interface{} {
	ac := cfg.Plugins.A2A
	m := map[string]interface{}{
		"streaming":          ac.Streaming,
		"push_notifications": ac.PushNotifications,
	}
	if ac.Endpoint != "" {
		m["endpoint"] = ac.Endpoint
	}
	if ac.PushURL != "" {
		m["push_url"] = ac.PushURL
	}
	if ac.TaskTTL != "" {
		m["task_ttl"] = ac.TaskTTL
	}
	if ac.MaxTasks > 0 {
		m["max_tasks"] = ac.MaxTasks
	}
	if ac.DBPath != "" {
		m["db_path"] = ac.DBPath
	}
	if cfg.Gateway.Origin.URL != "" {
		m["origin_url"] = cfg.Gateway.Origin.URL
	}
	return m
}

// agUIConfigMap converts AgUIConfig into a generic map for the plugin.
func agUIConfigMap(cfg *config.Config) map[string]interface{} {
	m := map[string]interface{}{}
	if cfg.Plugins.AgUI.Endpoint != "" {
		m["endpoint"] = cfg.Plugins.AgUI.Endpoint
	}
	return m
}

func a2aDetail(cfg *config.Config) string {
	endpoint := cfg.Plugins.A2A.Endpoint
	if endpoint == "" {
		endpoint = "/a2a"
	}
	detail := fmt.Sprintf("JSON-RPC at %s", endpoint)
	if cfg.Plugins.A2A.Streaming {
		detail += " (streaming)"
	}
	return detail
}

func agUIDetail(cfg *config.Config) string {
	endpoint := cfg.Plugins.AgUI.Endpoint
	if endpoint == "" {
		endpoint = "/ag-ui"
	}
	return fmt.Sprintf("SSE streaming at %s", endpoint)
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

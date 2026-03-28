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

	printBanner(cmd, cfg)

	addr := fmt.Sprintf("%s:%d", cfg.Gateway.Listen.Host, cfg.Gateway.Listen.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: p,
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
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
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

func analyticsDetail(cfg *config.Config) string {
	if cfg.Plugins.Analytics.LogFile != "" {
		return fmt.Sprintf("logging to %s", cfg.Plugins.Analytics.LogFile)
	}
	if cfg.Plugins.Analytics.Endpoint != "" {
		return fmt.Sprintf("reporting to %s", cfg.Plugins.Analytics.Endpoint)
	}
	return "enabled"
}

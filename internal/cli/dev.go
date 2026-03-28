package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var devConfigPath string

var devCmd = &cobra.Command{
	Use:   "dev",
	Short: "Start the gateway in development mode with verbose logging and auto-reload",
	Long: `Dev mode starts the gateway with:
  - Verbose (DEBUG) logging
  - Auto-reload on config file changes (fsnotify)
  - Origin health check on startup
  - Pretty-printed colored request logs`,
	RunE: runDev,
}

func init() {
	devCmd.Flags().StringVarP(&devConfigPath, "config", "c", "gateway.yaml", "path to config file")
	rootCmd.AddCommand(devCmd)
}

func runDev(cmd *cobra.Command, args []string) error {
	// Set up colored dev-mode log handler.
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	slog.SetDefault(slog.New(handler))

	fmt.Fprintln(cmd.OutOrStdout(), "  [dev] Development mode enabled — verbose logging, auto-reload on config changes")
	fmt.Fprintln(cmd.OutOrStdout())

	// Check origin health before starting.
	checkOriginHealth(cmd, devConfigPath)

	return startServer(cmd, devConfigPath, true)
}

// checkOriginHealth attempts to reach the configured origin URL and reports status.
func checkOriginHealth(cmd *cobra.Command, cfgPath string) {
	cfg, err := loadConfigQuick(cfgPath)
	if err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "  [dev] Could not load config for health check: %v\n", err)
		return
	}

	originURL := cfg.Gateway.Origin.URL
	if originURL == "" {
		return
	}

	fmt.Fprintf(cmd.OutOrStdout(), "  [dev] Checking origin health: %s ...", originURL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, originURL, nil)
	if err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), " ERROR (%v)\n", err)
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), " UNREACHABLE (%v)\n", err)
		fmt.Fprintln(cmd.OutOrStdout(), "  [dev] WARNING: Origin is not reachable. Gateway will return 502 for proxied requests.")
		fmt.Fprintln(cmd.OutOrStdout())
		return
	}
	resp.Body.Close()

	fmt.Fprintf(cmd.OutOrStdout(), " OK (HTTP %d)\n", resp.StatusCode)
	fmt.Fprintln(cmd.OutOrStdout())
}

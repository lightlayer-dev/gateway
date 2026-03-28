package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/lightlayer-dev/gateway/internal/config"
	"github.com/spf13/cobra"
)

var statusConfigPath string

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the running gateway status via the admin API",
	RunE:  runStatus,
}

func init() {
	statusCmd.Flags().StringVarP(&statusConfigPath, "config", "c", "gateway.yaml", "path to config file")
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfigQuick(statusConfigPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if !cfg.Admin.Enabled {
		return fmt.Errorf("admin API is disabled in config")
	}

	adminURL := fmt.Sprintf("http://localhost:%d/api/status", cfg.Admin.Port)

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodGet, adminURL, nil)
	if err != nil {
		return err
	}
	if cfg.Admin.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Admin.AuthToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach admin API at %s: %w", adminURL, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var status map[string]interface{}
	if err := json.Unmarshal(body, &status); err != nil {
		return fmt.Errorf("invalid response: %s", string(body))
	}

	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "\n ⚡ LightLayer Gateway Status\n\n")
	fmt.Fprintf(w, "  Status:   %v\n", status["status"])
	fmt.Fprintf(w, "  Version:  %v\n", status["version"])
	fmt.Fprintf(w, "  Uptime:   %v\n", status["uptime"])
	fmt.Fprintf(w, "  Origin:   %v\n", status["origin_url"])

	if plugins, ok := status["plugins"].([]interface{}); ok {
		fmt.Fprintln(w, "\n  Active Plugins:")
		for _, p := range plugins {
			if pm, ok := p.(map[string]interface{}); ok {
				fmt.Fprintf(w, "    ✓ %v\n", pm["name"])
			}
		}
	}

	if reqs, ok := status["total_requests"]; ok {
		fmt.Fprintf(w, "\n  Total Requests: %v\n", reqs)
	}
	fmt.Fprintln(w)

	return nil
}

// loadConfigQuick loads a config file without defaults/validation, for quick reads.
func loadConfigQuick(path string) (*config.Config, error) {
	cfg, err := config.LoadConfig(path)
	if err != nil {
		// Fall back to basic load without validation (for status check).
		return config.Load(path)
	}
	return cfg, nil
}

package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lightlayer-dev/gateway/internal/demo"
	"github.com/spf13/cobra"
)

var (
	demoPort      int
	demoAdminPort int
)

var demoCmd = &cobra.Command{
	Use:   "demo",
	Short: "Start the gateway in demo mode with a built-in sample API",
	Long: `Demo mode starts a built-in sample API and configures the gateway to proxy it
with all agent-readiness features enabled. No external API needed.

Perfect for:
  - Exploring the gateway dashboard
  - Seeing agent-readiness features in action
  - Testing before connecting your own API`,
	RunE: runDemo,
}

func init() {
	demoCmd.Flags().IntVar(&demoPort, "port", 8080, "gateway proxy port")
	demoCmd.Flags().IntVar(&demoAdminPort, "admin-port", 9090, "admin dashboard port")
	rootCmd.AddCommand(demoCmd)
}

func runDemo(cmd *cobra.Command, args []string) error {
	// Start the built-in demo API.
	demoAPI := demo.NewServer()
	apiPort, err := demoAPI.Start()
	if err != nil {
		return fmt.Errorf("failed to start demo API: %w", err)
	}
	defer demoAPI.Close()

	// Create a temporary config file.
	tmpDir, err := os.MkdirTemp("", "lightlayer-demo-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	cfgPath := filepath.Join(tmpDir, "gateway.yaml")
	dbPath := filepath.Join(tmpDir, "demo-analytics.db")

	cfgContent := fmt.Sprintf(`# LightLayer Gateway — Demo Mode
# Auto-generated config proxying the built-in demo API

gateway:
  listen:
    port: %d
    host: 0.0.0.0
  origin:
    url: http://127.0.0.1:%d
    timeout: 10s

plugins:
  discovery:
    enabled: true
    name: "Demo Products API"
    description: "A sample REST API for products and users — powered by LightLayer Gateway demo mode"
    version: "1.0.0"
    capabilities:
      - name: "products"
        description: "Product catalog — list, search, and create products"
        methods: ["GET", "POST"]
        paths: ["/products", "/products/*"]
      - name: "users"
        description: "User directory — list and look up users"
        methods: ["GET"]
        paths: ["/users", "/users/*"]
      - name: "health"
        description: "Service health check"
        methods: ["GET"]
        paths: ["/health"]

  analytics:
    enabled: true
    db_path: "%s"
    track_all: true

  payments:
    enabled: false

admin:
  enabled: true
  port: %d
`, demoPort, apiPort, dbPath, demoAdminPort)

	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		return fmt.Errorf("failed to write demo config: %w", err)
	}

	// Print demo banner.
	w := cmd.OutOrStdout()
	fmt.Fprintln(w)
	fmt.Fprintf(w, " 🧪 LightLayer Gateway — Demo Mode\n\n")
	fmt.Fprintf(w, "  Demo API:     http://127.0.0.1:%d  (built-in sample API)\n", apiPort)
	fmt.Fprintf(w, "  Gateway:      http://localhost:%d   (proxying the demo API)\n", demoPort)
	fmt.Fprintf(w, "  Dashboard:    http://localhost:%d\n", demoAdminPort)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  The gateway is adding agent-readiness to the demo API:")
	fmt.Fprintln(w, "    ✓ discovery     /.well-known/agent.json, /llms.txt, /agents.txt")
	fmt.Fprintln(w, "    ✓ analytics     tracking all agent traffic")
	fmt.Fprintln(w, "    ✓ errors        structured JSON error responses")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  Try it:")
	fmt.Fprintf(w, "    curl http://localhost:%d/products\n", demoPort)
	fmt.Fprintf(w, "    curl http://localhost:%d/.well-known/agent.json\n", demoPort)
	fmt.Fprintf(w, "    curl http://localhost:%d/llms.txt\n", demoPort)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Open the dashboard: http://localhost:%d\n\n", demoAdminPort)

	// Set demo mode globals so startServer tells admin about it.
	demoModeEnabled = true
	demoModeAPIURL = demoAPI.URL()

	// Start the gateway using the same startServer path.
	return startServer(cmd, cfgPath, true)
}

// Package-level vars for demo mode (read by startServer).
var (
	demoModeEnabled bool
	demoModeAPIURL  string
)

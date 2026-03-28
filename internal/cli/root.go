package cli

import (
	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags.
var Version = "dev"

var rootCmd = &cobra.Command{
	Use:   "lightlayer-gateway",
	Short: "LightLayer Gateway — reverse proxy for AI agent traffic",
	Long:  "A standalone reverse proxy that handles identity verification, payment negotiation, discovery serving, rate limiting, and analytics for AI agent traffic.",
}

func init() {
	rootCmd.Version = Version
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

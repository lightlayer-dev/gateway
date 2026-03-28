package cli

import (
	"github.com/spf13/cobra"
)

var devConfigPath string

var devCmd = &cobra.Command{
	Use:   "dev",
	Short: "Start the gateway in development mode with verbose logging",
	RunE:  runDev,
}

func init() {
	devCmd.Flags().StringVarP(&devConfigPath, "config", "c", "gateway.yaml", "path to config file")
	rootCmd.AddCommand(devCmd)
}

func runDev(cmd *cobra.Command, args []string) error {
	return startServer(cmd, devConfigPath, true)
}

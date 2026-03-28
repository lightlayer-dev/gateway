package cli

import (
	"fmt"

	"github.com/lightlayer-dev/gateway/internal/config"
	"github.com/spf13/cobra"
)

var validateConfigPath string

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate a gateway.yaml config file",
	RunE:  runValidate,
}

func init() {
	validateCmd.Flags().StringVarP(&validateConfigPath, "config", "c", "gateway.yaml", "path to config file")
	rootCmd.AddCommand(validateCmd)
}

func runValidate(cmd *cobra.Command, args []string) error {
	_, err := config.LoadConfig(validateConfigPath)
	if err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Config is valid.")
	return nil
}

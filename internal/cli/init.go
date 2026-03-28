package cli

import (
	"fmt"
	"os"

	"github.com/lightlayer-dev/gateway/configs"
	"github.com/spf13/cobra"
)

var initForce bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a gateway.yaml config file from the default template",
	RunE:  runInit,
}

func init() {
	initCmd.Flags().BoolVar(&initForce, "force", false, "overwrite existing gateway.yaml")
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	const path = "gateway.yaml"

	if !initForce {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists (use --force to overwrite)", path)
		}
	}

	if err := os.WriteFile(path, configs.GatewayYAML, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", path)
	return nil
}

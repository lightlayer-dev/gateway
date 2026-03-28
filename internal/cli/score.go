package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/lightlayer-dev/gateway/internal/score"
	"github.com/spf13/cobra"
)

var (
	scoreVerbose bool
	scoreJSON    bool
	scoreTimeout time.Duration
)

var scoreCmd = &cobra.Command{
	Use:   "score <url>",
	Short: "Score an API's agent-readiness (0-100)",
	Long:  "Lighthouse-style scanner that checks how well an API supports AI agents. Evaluates discovery endpoints, error format, rate limit headers, security, and more.",
	Args:  cobra.ExactArgs(1),
	RunE:  runScore,
}

func init() {
	scoreCmd.Flags().BoolVarP(&scoreVerbose, "verbose", "v", false, "show detailed suggestions per check")
	scoreCmd.Flags().BoolVar(&scoreJSON, "json", false, "output machine-readable JSON")
	scoreCmd.Flags().DurationVar(&scoreTimeout, "timeout", score.DefaultTimeout, "per-request timeout")
	rootCmd.AddCommand(scoreCmd)
}

func runScore(cmd *cobra.Command, args []string) error {
	targetURL := args[0]

	if !scoreJSON {
		fmt.Fprintf(cmd.OutOrStdout(), "\n%sScanning %s...%s\n", "\033[1m", targetURL, "\033[0m")
	}

	report, err := score.Scan(targetURL, scoreTimeout)
	if err != nil {
		return fmt.Errorf("scan failed: %w", err)
	}

	if scoreJSON {
		out, err := score.FormatJSON(report)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), out)
		return nil
	}

	fmt.Fprint(cmd.OutOrStdout(), score.FormatReport(report, scoreVerbose))

	// Show gateway improvement estimate.
	estimated := score.EstimateWithGateway(report)
	if estimated > report.Score {
		fmt.Fprint(cmd.OutOrStdout(), score.FormatGatewayEstimate(report.Score, estimated))
	}

	// Exit with non-zero if score is very low.
	if report.Score < 20 {
		os.Exit(1)
	}

	return nil
}

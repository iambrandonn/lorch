package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var resumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Resume a previous run",
	Long:  `Resume a previous run from its saved state and ledger.`,
	RunE:  runResume,
}

func init() {
	resumeCmd.Flags().StringP("run", "r", "", "Run ID to resume (required)")
	resumeCmd.MarkFlagRequired("run")
}

func runResume(cmd *cobra.Command, args []string) error {
	runID, err := cmd.Flags().GetString("run")
	if err != nil {
		return err
	}

	// Phase 1.1: Not yet implemented
	return fmt.Errorf("resume functionality not yet implemented (Phase 1.3+), run_id: %s", runID)
}

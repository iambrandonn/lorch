package cli

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "lorch",
	Short: "Local orchestrator for multi-agent workflows",
	Long: `lorch is a local-first orchestrator that coordinates multiple AI agents
(builder, reviewer, spec-maintainer, and optionally orchestration/NL intake)
to implement spec-driven development tasks.

Running 'lorch' without a subcommand is equivalent to 'lorch run'.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Default behavior: run the 'run' command
		return runCmd.RunE(cmd, args)
	},
}

func init() {
	// Add subcommands
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(resumeCmd)

	// Global flags
	rootCmd.PersistentFlags().StringP("config", "c", "", "Path to lorch.json config file (default: search up directory tree)")
}

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestRootCommandIncludesRunFlags(t *testing.T) {
	taskFlag := lookupFlag(rootCmd, "task")
	require.NotNil(t, taskFlag, "root command should expose the --task flag")
	require.Equal(t, "t", taskFlag.Shorthand, "root task flag shorthand mismatch")
}

func TestRootCommandDelegatesToRun(t *testing.T) {
	originalRunE := runCmd.RunE
	t.Cleanup(func() {
		runCmd.RunE = originalRunE
		resetFlag(rootCmd, "task")
		rootCmd.SetArgs(nil)
	})

	called := false
	runCmd.RunE = func(cmd *cobra.Command, args []string) error {
		called = true
		task, err := cmd.Flags().GetString("task")
		require.NoError(t, err)
		require.Equal(t, "T-CLI-TEST", task)
		return nil
	}

	rootCmd.SetArgs([]string{"--task", "T-CLI-TEST"})
	err := rootCmd.Execute()
	require.NoError(t, err)
	require.True(t, called, "root command should delegate to run command")
}

func resetFlag(cmd *cobra.Command, name string) {
	if flag := lookupFlag(cmd, name); flag != nil {
		_ = flag.Value.Set(flag.DefValue)
		flag.Changed = false
	}
}

func lookupFlag(cmd *cobra.Command, name string) *pflag.Flag {
	if flag := cmd.Flags().Lookup(name); flag != nil {
		return flag
	}
	return cmd.PersistentFlags().Lookup(name)
}

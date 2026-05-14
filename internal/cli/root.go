package cli

import "github.com/spf13/cobra"

var (
	cfgFile   string
	tokenFlag string
	logLevel  string
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "ghr",
		Short:         "GitHub Actions runner controller for macOS",
		Long:          "ghr manages ephemeral GitHub Actions runners via scale sets on macOS.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.PersistentFlags().StringVar(&cfgFile, "config", "", "path to config file")
	cmd.PersistentFlags().StringVar(&tokenFlag, "token", "", "override auth token for this invocation")
	cmd.PersistentFlags().StringVar(&logLevel, "log-level", "", "override log level (debug/info/warn/error)")

	cmd.AddCommand(
		newStartCmd(),
		newStopCmd(),
		newRestartCmd(),
		newRunCmd(),
		newStatusCmd(),
		newPurgeCmd(),
		newLoginCmd(),
		newLogoutCmd(),
		newAuthCmd(),
	)

	return cmd
}

func Execute() error {
	return newRootCmd().Execute()
}

package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the ghr daemon via launchd",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("ghr start: not yet implemented")
			return nil
		},
	}

	cmd.Flags().Bool("foreground", false, "run in foreground (same as 'ghr run')")

	return cmd
}

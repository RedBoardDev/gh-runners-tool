package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func newStopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the ghr daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("ghr stop: not yet implemented")
			return nil
		},
	}

	cmd.Flags().Duration("timeout", 30*time.Second, "max wait for graceful shutdown")
	cmd.Flags().Bool("force", false, "skip SIGTERM, send SIGKILL immediately")

	return cmd
}

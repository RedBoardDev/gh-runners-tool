package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show ghr daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("ghr status: not yet implemented")
			return nil
		},
	}

	cmd.Flags().Bool("json", false, "output in JSON format")
	cmd.Flags().Bool("watch", false, "live refresh mode")
	cmd.Flags().Duration("interval", 5*time.Second, "refresh interval for --watch")

	return cmd
}

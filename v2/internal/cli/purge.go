package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func newPurgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "purge",
		Short: "Stop everything, delete scale sets, clean workdirs",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("ghr purge: not yet implemented")
			return nil
		},
	}

	cmd.Flags().Duration("timeout", 5*time.Minute, "max wait for busy runners")
	cmd.Flags().Bool("force", false, "don't wait for busy runners")

	return cmd
}

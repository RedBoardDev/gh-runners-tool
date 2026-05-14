package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the ghr daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("ghr restart: not yet implemented")
			return nil
		},
	}
}

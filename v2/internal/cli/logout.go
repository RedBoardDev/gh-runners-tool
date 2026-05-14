package cli

import (
	"fmt"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/auth"
	"github.com/spf13/cobra"
)

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove saved credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := auth.Remove(); err != nil {
				return err
			}
			fmt.Println("Credentials removed")
			return nil
		},
	}
}

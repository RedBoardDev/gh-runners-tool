package cli

import (
	"fmt"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/auth"
	"github.com/spf13/cobra"
)

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authentication management",
	}

	cmd.AddCommand(newAuthStatusCmd())
	return cmd
}

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Display current authentication state",
		RunE: func(cmd *cobra.Command, args []string) error {
			creds, source, err := auth.Load(auth.LoadOpts{TokenFlag: tokenFlag})
			if err != nil { //nolint:nilerr -- intentional: show friendly message instead of error
				fmt.Println("Status:  not authenticated")
				fmt.Println("Run 'ghr login' to authenticate.")
				return nil
			}

			fmt.Printf("Method:  %s\n", creds.Method)
			fmt.Printf("Source:  %s\n", source)
			if creds.GitHubURL != "" {
				fmt.Printf("GitHub:  %s\n", creds.GitHubURL)
			}
			if creds.Method == "pat" && creds.PAT != "" {
				fmt.Printf("Token:   %s\n", auth.MaskedPAT(creds.PAT))
			}
			if creds.GitHubApp != nil {
				fmt.Printf("Client:  %s\n", creds.GitHubApp.ClientID)
				fmt.Printf("Install: %d\n", creds.GitHubApp.InstallationID)
				fmt.Printf("Key:     %s\n", creds.GitHubApp.PrivateKeyPath)
			}

			result, err := auth.Validate(cmd.Context(), creds)
			if err != nil {
				fmt.Printf("Status:  validation failed: %v\n", err)
				return nil
			}
			if result.Valid {
				fmt.Println("Status:  authenticated")
				if result.Username != "" {
					fmt.Printf("User:    @%s\n", result.Username)
				}
			}

			return nil
		},
	}
}

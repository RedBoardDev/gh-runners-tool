package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/auth"
	"github.com/spf13/cobra"
)

func newLoginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with GitHub",
		Long:  "Interactive wizard to configure GitHub authentication. Supports PAT and GitHub App.",
		RunE:  runLogin,
	}

	cmd.Flags().String("method", "", "auth method: pat or app")
	cmd.Flags().String("url", "", "GitHub URL (org, repo, or enterprise)")
	cmd.Flags().String("client-id", "", "GitHub App client ID")
	cmd.Flags().Int64("installation-id", 0, "GitHub App installation ID")
	cmd.Flags().String("private-key", "", "path to GitHub App private key (.pem)")

	return cmd
}

func runLogin(cmd *cobra.Command, _ []string) error {
	method, err := cmd.Flags().GetString("method")
	if err != nil {
		return fmt.Errorf("get method flag: %w", err)
	}

	if method == "" {
		reader := bufio.NewReader(os.Stdin)
		return interactiveLogin(cmd, reader)
	}

	return nonInteractiveLogin(cmd, method)
}

func nonInteractiveLogin(cmd *cobra.Command, method string) error {
	url, err := cmd.Flags().GetString("url")
	if err != nil {
		return fmt.Errorf("get url flag: %w", err)
	}

	var creds *auth.Credentials

	switch method {
	case "pat":
		if tokenFlag == "" {
			return fmt.Errorf("--token is required for PAT authentication")
		}
		if url == "" {
			return fmt.Errorf("--url is required")
		}
		creds = &auth.Credentials{
			Method:    "pat",
			GitHubURL: url,
			PAT:       tokenFlag,
		}

	case "app":
		clientID, flagErr := cmd.Flags().GetString("client-id")
		if flagErr != nil {
			return fmt.Errorf("get client-id flag: %w", flagErr)
		}
		installationID, flagErr := cmd.Flags().GetInt64("installation-id")
		if flagErr != nil {
			return fmt.Errorf("get installation-id flag: %w", flagErr)
		}
		privateKey, flagErr := cmd.Flags().GetString("private-key")
		if flagErr != nil {
			return fmt.Errorf("get private-key flag: %w", flagErr)
		}
		if clientID == "" || installationID == 0 || privateKey == "" || url == "" {
			return fmt.Errorf("--client-id, --installation-id, --private-key, and --url are all required for GitHub App authentication")
		}
		creds = &auth.Credentials{
			Method:    "github_app",
			GitHubURL: url,
			GitHubApp: &auth.GitHubAppCreds{
				ClientID:       clientID,
				InstallationID: installationID,
				PrivateKeyPath: privateKey,
			},
		}

	default:
		return fmt.Errorf("unknown method %q: must be 'pat' or 'app'", method)
	}

	return validateAndSave(cmd, creds)
}

func validateAndSave(cmd *cobra.Command, creds *auth.Credentials) error {
	fmt.Println("  Validating...")
	result, err := auth.Validate(cmd.Context(), creds)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	if !result.Valid {
		return fmt.Errorf("credentials are not valid")
	}

	if err := auth.Save(creds); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}

	if creds.Method == "pat" && result.Username != "" {
		fmt.Printf("✓ Authenticated as @%s\n", result.Username)
	}
	if creds.Method == "pat" && len(result.Scopes) > 0 {
		fmt.Printf("✓ Scopes: %s\n", strings.Join(result.Scopes, ", "))
	}
	fmt.Printf("✓ Credentials saved to %s\n", auth.FilePath())

	return nil
}

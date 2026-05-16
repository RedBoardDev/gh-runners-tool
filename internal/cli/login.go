package cli

import (
	"bufio"
	"fmt"
	"os"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/auth"
	"github.com/spf13/cobra"
)

func newLoginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with GitHub",
		Long:  "Interactive wizard to configure GitHub authentication. GitHub App (recommended) or PAT.",
		RunE:  runLogin,
	}

	cmd.Flags().String("method", "", "auth method: app or pat (interactive if empty)")
	cmd.Flags().String("url", "", "GitHub URL for PAT mode (org or repo)")
	cmd.Flags().String("host", "", "GitHub host URL for App mode (default https://github.com)")
	cmd.Flags().String("client-id", "", "GitHub App client ID")
	cmd.Flags().Int64("installation-id", 0, "GitHub App installation ID (auto-detected if only one)")
	cmd.Flags().String("private-key", "", "path to GitHub App private key (.pem)")
	return cmd
}

func runLogin(cmd *cobra.Command, _ []string) error {
	method, err := cmd.Flags().GetString("method")
	if err != nil {
		return fmt.Errorf("get method flag: %w", err)
	}
	if method == "" {
		return interactiveLogin(cmd, bufio.NewReader(os.Stdin))
	}
	return nonInteractiveLogin(cmd, method)
}

func nonInteractiveLogin(cmd *cobra.Command, method string) error {
	switch method {
	case "pat":
		return nonInteractivePAT(cmd)
	case "app", "github_app":
		return nonInteractiveApp(cmd)
	default:
		return fmt.Errorf("unknown method %q (expected 'app' or 'pat')", method)
	}
}

func nonInteractivePAT(cmd *cobra.Command) error {
	if tokenFlag == "" {
		return fmt.Errorf("--token is required for PAT method")
	}
	url, err := cmd.Flags().GetString("url")
	if err != nil {
		return fmt.Errorf("get url flag: %w", err)
	}
	if url == "" {
		return fmt.Errorf("--url is required for PAT method")
	}
	creds := &auth.Credentials{
		Method:    "pat",
		GitHubURL: url,
		PAT:       tokenFlag,
	}
	return validateAndSave(cmd, creds)
}

func nonInteractiveApp(cmd *cobra.Command) error {
	clientID, err := cmd.Flags().GetString("client-id")
	if err != nil {
		return fmt.Errorf("get client-id flag: %w", err)
	}
	privateKey, err := cmd.Flags().GetString("private-key")
	if err != nil {
		return fmt.Errorf("get private-key flag: %w", err)
	}
	host, err := cmd.Flags().GetString("host")
	if err != nil {
		return fmt.Errorf("get host flag: %w", err)
	}
	installationID, err := cmd.Flags().GetInt64("installation-id")
	if err != nil {
		return fmt.Errorf("get installation-id flag: %w", err)
	}

	in := appLoginInput{
		clientID:       clientID,
		privateKeyPath: expandHome(privateKey),
		hostURL:        host,
		installationID: installationID,
	}
	prep, err := prepareAppLogin(cmd.Context(), in)
	if err != nil {
		return err
	}
	inst, err := resolveInstallation(prep.installations, in.installationID)
	if err != nil {
		return err
	}
	creds, err := finalizeAppLogin(cmd.Context(), prep, inst, in)
	if err != nil {
		return err
	}
	return saveCreds(creds)
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
	fmt.Printf("✓ Credentials saved to %s\n", auth.FilePath())
	return nil
}

func saveCreds(creds *auth.Credentials) error {
	if err := auth.Save(creds); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}
	fmt.Println()
	fmt.Println("✓ Authentication successful")
	if creds.GitHubApp != nil {
		fmt.Printf("  Method:       github_app\n")
		fmt.Printf("  Account:      @%s\n", creds.GitHubApp.Account)
		fmt.Printf("  Installation: %d\n", creds.GitHubApp.InstallationID)
		fmt.Printf("  URL:          %s\n", creds.GitHubURL)
		fmt.Printf("  Key:          %s\n", creds.GitHubApp.PrivateKeyPath)
	}
	fmt.Printf("  Saved to:     %s\n", auth.FilePath())
	return nil
}

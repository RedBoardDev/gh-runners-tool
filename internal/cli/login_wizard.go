package cli

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/auth"
	"github.com/spf13/cobra"
)

func interactiveLogin(cmd *cobra.Command, reader *bufio.Reader) error {
	fmt.Println()
	fmt.Println("Authentication method:")
	fmt.Println("  1) GitHub App  (recommended — short-lived tokens, scoped permissions)")
	fmt.Println("  2) Personal Access Token")
	choice, err := readLine(reader, "Choose [1]")
	if err != nil {
		return err
	}
	switch choice {
	case "", "1":
		return interactiveApp(cmd, reader)
	case "2":
		return interactivePAT(cmd, reader)
	default:
		return fmt.Errorf("invalid choice %q (expected 1 or 2)", choice)
	}
}

func interactivePAT(cmd *cobra.Command, reader *bufio.Reader) error {
	token, err := readLine(reader, "GitHub PAT")
	if err != nil {
		return err
	}
	if token == "" {
		return fmt.Errorf("token cannot be empty")
	}
	url, err := readLine(reader, "GitHub URL (org or repo)")
	if err != nil {
		return err
	}
	if url == "" {
		return fmt.Errorf("URL cannot be empty")
	}
	creds := &auth.Credentials{
		Method:    "pat",
		GitHubURL: url,
		PAT:       token,
	}
	return validateAndSave(cmd, creds)
}

func interactiveApp(cmd *cobra.Command, reader *bufio.Reader) error {
	fmt.Println()
	fmt.Println("Don't have a GitHub App yet? Create one at:")
	fmt.Println("  https://github.com/organizations/YOUR_ORG/settings/apps/new")
	fmt.Println("Required: Organization permissions → Self-hosted runners → Read & Write")
	fmt.Println("Then generate a .pem private key (chmod 600) and install the App.")
	fmt.Println()

	clientID, err := readLine(reader, "GitHub App Client ID")
	if err != nil {
		return err
	}
	pemPath, err := readLine(reader, "Path to private key (.pem)")
	if err != nil {
		return err
	}
	hostURL, err := readLine(reader, "GitHub host URL [https://github.com]")
	if err != nil {
		return err
	}

	in := appLoginInput{
		clientID:       clientID,
		privateKeyPath: expandHome(pemPath),
		hostURL:        hostURL,
	}

	fmt.Println("  Validating credentials...")
	prep, err := prepareAppLogin(cmd.Context(), in)
	if err != nil {
		return err
	}
	fmt.Printf("  Found %d installation(s)\n", len(prep.installations))

	inst, err := selectInstallation(reader, prep.installations)
	if err != nil {
		return err
	}

	fmt.Println("  Generating installation token...")
	creds, err := finalizeAppLogin(cmd.Context(), prep, inst, in)
	if err != nil {
		return err
	}
	return saveCreds(creds)
}

func readLine(reader *bufio.Reader, label string) (string, error) {
	if label != "" {
		fmt.Printf("? %s: ", label)
	}
	raw, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read input: %w", err)
	}
	return strings.TrimSpace(raw), nil
}

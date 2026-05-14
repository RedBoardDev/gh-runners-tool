package cli

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/auth"
	"github.com/spf13/cobra"
)

func interactiveLogin(cmd *cobra.Command, reader *bufio.Reader) error {
	fmt.Println()
	fmt.Println("? Authentication method")
	fmt.Println("  1) Personal Access Token (PAT)")
	fmt.Println("  2) GitHub App")
	fmt.Print("> ")

	choice, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read choice: %w", err)
	}
	choice = strings.TrimSpace(choice)

	switch choice {
	case "1":
		return interactivePAT(cmd, reader)
	case "2":
		return interactiveApp(cmd, reader)
	default:
		return fmt.Errorf("invalid choice: %q (expected 1 or 2)", choice)
	}
}

func interactivePAT(cmd *cobra.Command, reader *bufio.Reader) error {
	fmt.Print("? GitHub PAT: ")
	token, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read token: %w", err)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("token cannot be empty")
	}

	fmt.Print("? GitHub URL (org or repo): ")
	url, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read url: %w", err)
	}
	url = strings.TrimSpace(url)
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
	fmt.Print("? GitHub App Client ID: ")
	clientID, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read client ID: %w", err)
	}
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return fmt.Errorf("client ID cannot be empty")
	}

	fmt.Print("? Installation ID: ")
	installIDStr, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read installation ID: %w", err)
	}
	installID, err := strconv.ParseInt(strings.TrimSpace(installIDStr), 10, 64)
	if err != nil {
		return fmt.Errorf("parse installation ID: %w", err)
	}

	fmt.Print("? Private key path (.pem): ")
	keyPath, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read private key path: %w", err)
	}
	keyPath = strings.TrimSpace(keyPath)
	if keyPath == "" {
		return fmt.Errorf("private key path cannot be empty")
	}

	fmt.Print("? GitHub URL: ")
	url, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read url: %w", err)
	}
	url = strings.TrimSpace(url)
	if url == "" {
		return fmt.Errorf("URL cannot be empty")
	}

	creds := &auth.Credentials{
		Method:    "github_app",
		GitHubURL: url,
		GitHubApp: &auth.GitHubAppCreds{
			ClientID:       clientID,
			InstallationID: installID,
			PrivateKeyPath: keyPath,
		},
	}

	return validateAndSave(cmd, creds)
}

package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/auth"
)

type appLoginInput struct {
	clientID       string
	privateKeyPath string
	hostURL        string
	installationID int64
}

type appLoginPrepared struct {
	apiBase       string
	jwt           string
	installations []auth.Installation
}

func prepareAppLogin(ctx context.Context, in appLoginInput) (*appLoginPrepared, error) {
	if in.clientID == "" {
		return nil, fmt.Errorf("client ID is required")
	}
	if in.privateKeyPath == "" {
		return nil, fmt.Errorf("private key path is required")
	}
	if in.hostURL == "" {
		in.hostURL = "https://github.com"
	}

	pemBytes, err := auth.LoadPrivateKey(in.privateKeyPath)
	if err != nil {
		return nil, err
	}
	jwtToken, err := auth.SignAppJWT(in.clientID, pemBytes)
	if err != nil {
		return nil, err
	}
	apiBase, err := auth.APIBaseURL(in.hostURL)
	if err != nil {
		return nil, err
	}
	installations, err := auth.ListAppInstallations(ctx, apiBase, jwtToken)
	if err != nil {
		return nil, err
	}
	if len(installations) == 0 {
		return nil, fmt.Errorf("the GitHub App has no installations — install it on an org or repo first at https://github.com/settings/installations")
	}
	return &appLoginPrepared{apiBase: apiBase, jwt: jwtToken, installations: installations}, nil
}

func resolveInstallation(installations []auth.Installation, requestedID int64) (*auth.Installation, error) {
	if requestedID != 0 {
		for i := range installations {
			if installations[i].ID == requestedID {
				return &installations[i], nil
			}
		}
		return nil, fmt.Errorf("installation %d not found (available: %s)", requestedID, formatInstallationList(installations))
	}
	if len(installations) == 1 {
		return &installations[0], nil
	}
	return nil, fmt.Errorf("multiple installations found, pass --installation-id (available: %s)", formatInstallationList(installations))
}

func selectInstallation(reader *bufio.Reader, installations []auth.Installation) (*auth.Installation, error) {
	if len(installations) == 1 {
		fmt.Printf("  Using installation @%s (id %d)\n", installations[0].Account, installations[0].ID)
		return &installations[0], nil
	}
	fmt.Println()
	fmt.Println("Available installations:")
	for i, inst := range installations {
		fmt.Printf("  %d) @%s (%s, id %d)\n", i+1, inst.Account, strings.ToLower(inst.AccountType), inst.ID)
	}
	fmt.Print("? Select installation: ")
	raw, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("read selection: %w", err)
	}
	idx := 0
	if _, err := fmt.Sscanf(strings.TrimSpace(raw), "%d", &idx); err != nil || idx < 1 || idx > len(installations) {
		return nil, fmt.Errorf("invalid selection %q", strings.TrimSpace(raw))
	}
	return &installations[idx-1], nil
}

func finalizeAppLogin(ctx context.Context, prep *appLoginPrepared, inst *auth.Installation, in appLoginInput) (*auth.Credentials, error) {
	token, err := auth.IssueInstallationToken(ctx, prep.apiBase, prep.jwt, inst.ID)
	if err != nil {
		return nil, err
	}
	if err := auth.CheckRunnerPermissions(token.Permissions); err != nil {
		return nil, err
	}
	host := strings.TrimRight(in.hostURL, "/")
	return &auth.Credentials{
		Method:    "github_app",
		GitHubURL: fmt.Sprintf("%s/%s", host, inst.Account),
		GitHubApp: &auth.GitHubAppCreds{
			ClientID:       in.clientID,
			InstallationID: inst.ID,
			PrivateKeyPath: in.privateKeyPath,
			Account:        inst.Account,
		},
	}, nil
}

func formatInstallationList(installations []auth.Installation) string {
	parts := make([]string, len(installations))
	for i, inst := range installations {
		parts[i] = fmt.Sprintf("%d (@%s)", inst.ID, inst.Account)
	}
	return strings.Join(parts, ", ")
}

func expandHome(path string) string {
	if !strings.HasPrefix(path, "~/") && path != "~" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

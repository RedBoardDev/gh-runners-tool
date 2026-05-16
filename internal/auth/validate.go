package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

func Validate(ctx context.Context, creds *Credentials) (*ValidationResult, error) {
	switch creds.Method {
	case "pat":
		return validatePAT(ctx, creds.PAT)
	case "github_app":
		return validateGitHubApp(creds.GitHubApp)
	default:
		return nil, fmt.Errorf("validate credentials: unknown method %q", creds.Method)
	}
}

func validatePAT(ctx context.Context, pat string) (*ValidationResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("validate PAT: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("validate PAT: request failed: %w", err)
	}
	defer drainBody(resp)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("validate PAT: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("validate PAT: GitHub API returned %d: %s", resp.StatusCode, truncateBody(string(body)))
	}

	var user githubUserResponse
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, fmt.Errorf("validate PAT: parse response: %w", err)
	}

	scopes := parseScopes(resp.Header.Get("X-OAuth-Scopes"))

	return &ValidationResult{
		Valid:    true,
		Username: user.Login,
		Scopes:   scopes,
	}, nil
}

func parseScopes(header string) []string {
	if header == "" {
		return nil
	}
	parts := strings.Split(header, ",")
	scopes := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s != "" {
			scopes = append(scopes, s)
		}
	}
	return scopes
}

func validateGitHubApp(app *GitHubAppCreds) (*ValidationResult, error) {
	if app == nil {
		return nil, fmt.Errorf("validate GitHub App: credentials are nil")
	}
	f, err := os.Open(app.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("validate GitHub App: open private key %s: %w", app.PrivateKeyPath, err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("validate GitHub App: close private key file: %w", err)
	}

	return &ValidationResult{
		Valid: true,
	}, nil
}

func MaskedPAT(pat string) string {
	if len(pat) < 12 {
		return "****"
	}
	return pat[:4] + "..." + pat[len(pat)-4:]
}

package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Installation struct {
	ID          int64
	Account     string
	AccountType string
	TargetType  string
	HTMLURL     string
}

type InstallationToken struct {
	Token       string
	ExpiresAt   string
	Permissions map[string]string
}

const (
	permAdministration = "administration"
	permOrgRunners     = "organization_self_hosted_runners"
)

func ListAppInstallations(ctx context.Context, apiBaseURL, appJWT string) ([]Installation, error) {
	endpoint := strings.TrimRight(apiBaseURL, "/") + "/app/installations?per_page=100"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create installations request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list installations: %w", err)
	}
	defer drainBody(resp)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read installations response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("list installations: GitHub rejected the JWT (check Client ID and private key belong to the same App)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list installations: HTTP %d: %s", resp.StatusCode, truncateBody(string(body)))
	}

	var raw []struct {
		ID      int64 `json:"id"`
		Account struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"account"`
		TargetType string `json:"target_type"`
		HTMLURL    string `json:"html_url"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode installations: %w", err)
	}

	out := make([]Installation, 0, len(raw))
	for _, r := range raw {
		out = append(out, Installation{
			ID:          r.ID,
			Account:     r.Account.Login,
			AccountType: r.Account.Type,
			TargetType:  r.TargetType,
			HTMLURL:     r.HTMLURL,
		})
	}
	return out, nil
}

func IssueInstallationToken(ctx context.Context, apiBaseURL, appJWT string, installationID int64) (*InstallationToken, error) {
	endpoint := fmt.Sprintf("%s/app/installations/%d/access_tokens", strings.TrimRight(apiBaseURL, "/"), installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create access_tokens request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("issue installation token: %w", err)
	}
	defer drainBody(resp)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read access_tokens response: %w", err)
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("issue installation token: HTTP %d: %s", resp.StatusCode, truncateBody(string(body)))
	}

	var raw struct {
		Token       string            `json:"token"`
		ExpiresAt   string            `json:"expires_at"`
		Permissions map[string]string `json:"permissions"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode installation token: %w", err)
	}
	return &InstallationToken{
		Token:       raw.Token,
		ExpiresAt:   raw.ExpiresAt,
		Permissions: raw.Permissions,
	}, nil
}

func CheckRunnerPermissions(perms map[string]string) error {
	if hasWrite(perms, permAdministration) || hasWrite(perms, permOrgRunners) {
		return nil
	}
	return fmt.Errorf(
		"GitHub App lacks runner permissions: enable %q OR %q with 'write' access in the App settings",
		permAdministration, permOrgRunners,
	)
}

func APIBaseURL(githubURL string) (string, error) {
	if githubURL == "" {
		return "https://api.github.com", nil
	}
	u, err := url.Parse(githubURL)
	if err != nil {
		return "", fmt.Errorf("parse github URL %q: %w", githubURL, err)
	}
	host := strings.ToLower(u.Host)
	if host == "" {
		return "", fmt.Errorf("github URL %q has no host", githubURL)
	}
	if host == "github.com" || host == "api.github.com" {
		return "https://api.github.com", nil
	}
	return fmt.Sprintf("%s://%s/api/v3", u.Scheme, u.Host), nil
}

func hasWrite(perms map[string]string, key string) bool {
	v, ok := perms[key]
	return ok && (v == "write" || v == "admin")
}

func drainBody(resp *http.Response) {
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

func truncateBody(s string) string {
	const maxBodyLen = 500
	if len(s) > maxBodyLen {
		return s[:maxBodyLen] + "..."
	}
	return s
}

package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"gh-runners-tool/internal/config"
)

type Client struct {
	httpClient *http.Client
	token      string
}

type registrationTokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// New creates a GitHub API client using a PAT from env.
func New(token string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 15 * time.Second},
		token:      token,
	}
}

type Runner struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Busy   bool   `json:"busy"`
}

type listRunnersResponse struct {
	Runners []Runner `json:"runners"`
}

// RegistrationToken requests a registration token for runners.
func (c *Client) RegistrationToken(ctx context.Context, gh config.GitHubConfig) (string, error) {
	url := ""
	switch gh.Scope {
	case config.ScopeOrg:
		url = fmt.Sprintf("https://api.github.com/orgs/%s/actions/runners/registration-token", gh.Owner)
	case config.ScopeRepo:
		url = fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/runners/registration-token", gh.Owner, gh.Repo)
	default:
		return "", fmt.Errorf("unknown scope %s", gh.Scope)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request registration token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("registration token failed: status %d", resp.StatusCode)
	}

	var decoded registrationTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if decoded.Token == "" {
		return "", fmt.Errorf("empty token returned")
	}
	return decoded.Token, nil
}

// ListRunners returns all runners for the configured scope (first page).
func (c *Client) ListRunners(ctx context.Context, gh config.GitHubConfig) ([]Runner, error) {
	var all []Runner
	page := 1

	for {
		url := ""
		switch gh.Scope {
		case config.ScopeOrg:
			url = fmt.Sprintf("https://api.github.com/orgs/%s/actions/runners?per_page=100&page=%d", gh.Owner, page)
		case config.ScopeRepo:
			url = fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/runners?per_page=100&page=%d", gh.Owner, gh.Repo, page)
		default:
			return nil, fmt.Errorf("unknown scope %s", gh.Scope)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("Authorization", "Bearer "+c.token)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("list runners: %w", err)
		}
		if resp.StatusCode >= 300 {
			resp.Body.Close()
			return nil, fmt.Errorf("list runners failed: status %d", resp.StatusCode)
		}

		var decoded listRunnersResponse
		if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode response: %w", err)
		}

		all = append(all, decoded.Runners...)
		resp.Body.Close()

		if len(decoded.Runners) < 100 {
			break
		}
		page++
	}

	return all, nil
}

// DeleteRunner removes a runner registration by ID.
func (c *Client) DeleteRunner(ctx context.Context, gh config.GitHubConfig, id int64) error {
	url := ""
	switch gh.Scope {
	case config.ScopeOrg:
		url = fmt.Sprintf("https://api.github.com/orgs/%s/actions/runners/%d", gh.Owner, id)
	case config.ScopeRepo:
		url = fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/runners/%d", gh.Owner, gh.Repo, id)
	default:
		return fmt.Errorf("unknown scope %s", gh.Scope)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete runner: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete runner failed: status %d body=%s", resp.StatusCode, string(body))
	}
	return nil
}

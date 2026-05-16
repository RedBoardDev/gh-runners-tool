package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type GitHubAPICheck struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

func (c GitHubAPICheck) Name() string { return "github-api" }

type rateLimitResponse struct {
	Resources struct {
		Core struct {
			Limit     int   `json:"limit"`
			Remaining int   `json:"remaining"`
			Reset     int64 `json:"reset"`
		} `json:"core"`
	} `json:"resources"`
}

func (c GitHubAPICheck) Run(ctx context.Context) Result {
	res := Result{Name: c.Name()}

	if c.Token == "" {
		res.Status = StatusSkip
		res.Summary = "no token available"
		res.Hint = "run 'ghr login' so doctor can probe the GitHub API"
		return res
	}

	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" || strings.Contains(base, "github.com") && !strings.Contains(base, "api.github.com") {
		base = "https://api.github.com"
	}
	url := base + "/rate_limit"
	res.Details = []string{url}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")

	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		res.Status = StatusFail
		res.Summary = fmt.Sprintf("request failed: %v", err)
		res.Hint = "check network connectivity and the configured github.url"
		return res
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		res.Status = StatusFail
		res.Summary = fmt.Sprintf("auth rejected (%d)", resp.StatusCode)
		res.Hint = "credentials may be expired or lack required scopes; rerun 'ghr login'"
		return res
	}
	if resp.StatusCode >= 400 {
		res.Status = StatusFail
		res.Summary = fmt.Sprintf("unexpected status %d", resp.StatusCode)
		return res
	}

	var body rateLimitResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		res.Status = StatusFail
		res.Summary = fmt.Sprintf("decode response: %v", err)
		return res
	}

	core := body.Resources.Core
	pct := 0
	if core.Limit > 0 {
		pct = (core.Remaining * 100) / core.Limit
	}
	resetIn := time.Until(time.Unix(core.Reset, 0)).Round(time.Second)
	res.Details = append(res.Details,
		fmt.Sprintf("core: %d/%d remaining (%d%%)", core.Remaining, core.Limit, pct),
		fmt.Sprintf("resets in: %s", resetIn),
	)
	switch {
	case pct >= 20:
		res.Status = StatusOK
		res.Summary = "api reachable, rate-limit healthy"
	default:
		res.Status = StatusWarn
		res.Summary = fmt.Sprintf("rate-limit low (%d%% remaining)", pct)
		res.Hint = "reduce poll frequency or wait for the window to reset"
	}
	return res
}

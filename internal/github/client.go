package github

import (
	"context"
	"fmt"
	"os"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/auth"
	"github.com/actions/scaleset"
	"github.com/actions/scaleset/listener"
)

var systemInfo = scaleset.SystemInfo{
	System:  "ghr",
	Version: "2.0",
}

type Client struct {
	inner *scaleset.Client
}

func NewClient(creds *auth.Credentials, githubURL string) (*Client, error) {
	switch creds.Method {
	case "pat":
		return newPATClient(creds.PAT, githubURL)
	case "github_app":
		return newAppClient(creds.GitHubApp, githubURL)
	default:
		return nil, fmt.Errorf("new github client: unknown auth method %q", creds.Method)
	}
}

func newPATClient(token, githubURL string) (*Client, error) {
	inner, err := scaleset.NewClientWithPersonalAccessToken(scaleset.NewClientWithPersonalAccessTokenConfig{
		GitHubConfigURL:     githubURL,
		PersonalAccessToken: token,
		SystemInfo:          systemInfo,
	})
	if err != nil {
		return nil, fmt.Errorf("create PAT client: %w", err)
	}
	return &Client{inner: inner}, nil
}

func newAppClient(app *auth.GitHubAppCreds, githubURL string) (*Client, error) {
	if app == nil {
		return nil, fmt.Errorf("create app client: github_app credentials are nil")
	}

	pemBytes, err := os.ReadFile(app.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read private key %s: %w", app.PrivateKeyPath, err)
	}

	inner, err := scaleset.NewClientWithGitHubApp(scaleset.ClientWithGitHubAppConfig{
		GitHubConfigURL: githubURL,
		GitHubAppAuth: scaleset.GitHubAppAuth{
			ClientID:       app.ClientID,
			InstallationID: app.InstallationID,
			PrivateKey:     string(pemBytes),
		},
		SystemInfo: systemInfo,
	})
	if err != nil {
		return nil, fmt.Errorf("create app client: %w", err)
	}
	return &Client{inner: inner}, nil
}

func (c *Client) CreateScaleSet(ctx context.Context, name string, runnerGroupID int, labels []string) (*scaleset.RunnerScaleSet, error) {
	sdkLabels := make([]scaleset.Label, len(labels))
	for i, l := range labels {
		sdkLabels[i] = scaleset.Label{Type: "System", Name: l}
	}

	ss, err := c.inner.CreateRunnerScaleSet(ctx, &scaleset.RunnerScaleSet{
		Name:          name,
		RunnerGroupID: runnerGroupID,
		Labels:        sdkLabels,
		RunnerSetting: scaleset.RunnerSetting{DisableUpdate: true},
	})
	if err != nil {
		return nil, fmt.Errorf("create scale set %q: %w", name, err)
	}
	return ss, nil
}

func (c *Client) GetScaleSet(ctx context.Context, runnerGroupID int, name string) (*scaleset.RunnerScaleSet, error) {
	ss, err := c.inner.GetRunnerScaleSet(ctx, runnerGroupID, name)
	if err != nil {
		return nil, fmt.Errorf("get scale set %q: %w", name, err)
	}
	return ss, nil
}

func (c *Client) GetScaleSetByID(ctx context.Context, id int) (*scaleset.RunnerScaleSet, error) {
	ss, err := c.inner.GetRunnerScaleSetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get scale set by id %d: %w", id, err)
	}
	return ss, nil
}

func (c *Client) DeleteScaleSet(ctx context.Context, id int) error {
	if err := c.inner.DeleteRunnerScaleSet(ctx, id); err != nil {
		return fmt.Errorf("delete scale set %d: %w", id, err)
	}
	return nil
}

func (c *Client) GenerateJITConfig(ctx context.Context, scaleSetID int, runnerName string) (string, int, error) {
	jit, err := c.inner.GenerateJitRunnerConfig(ctx, &scaleset.RunnerScaleSetJitRunnerSetting{
		Name: runnerName,
	}, scaleSetID)
	if err != nil {
		return "", 0, fmt.Errorf("generate JIT config for %q: %w", runnerName, err)
	}
	if jit.Runner == nil {
		return jit.EncodedJITConfig, 0, nil
	}
	return jit.EncodedJITConfig, jit.Runner.ID, nil
}

func (c *Client) RemoveRunner(ctx context.Context, runnerID int) error {
	if err := c.inner.RemoveRunner(ctx, int64(runnerID)); err != nil {
		return fmt.Errorf("remove runner %d: %w", runnerID, err)
	}
	return nil
}

func (c *Client) OpenSession(ctx context.Context, scaleSetID int, owner string) (*scaleset.MessageSessionClient, error) {
	session, err := c.inner.MessageSessionClient(ctx, scaleSetID, owner)
	if err != nil {
		return nil, fmt.Errorf("open session for scale set %d: %w", scaleSetID, err)
	}
	return session, nil
}

func (c *Client) NewListener(session *scaleset.MessageSessionClient, scaleSetID, maxRunners int) (*listener.Listener, error) {
	l, err := listener.New(session, listener.Config{
		ScaleSetID: scaleSetID,
		MaxRunners: maxRunners,
	})
	if err != nil {
		return nil, fmt.Errorf("create listener for scale set %d: %w", scaleSetID, err)
	}
	return l, nil
}

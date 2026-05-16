package auth

import (
	"log/slog"
	"time"
)

type Credentials struct {
	Method    string          `json:"method"`
	GitHubURL string          `json:"github_url"`
	PAT       string          `json:"pat,omitempty"`
	GitHubApp *GitHubAppCreds `json:"github_app,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

type GitHubAppCreds struct {
	ClientID       string `json:"client_id"`
	InstallationID int64  `json:"installation_id"`
	PrivateKeyPath string `json:"private_key_path"`
	Account        string `json:"account,omitempty"`
}

type LoadOpts struct {
	TokenFlag string
}

type ValidationResult struct {
	Valid    bool
	Username string
	Scopes   []string
	OrgName  string
}

type githubUserResponse struct {
	Login string `json:"login"`
}

func (c *Credentials) LogValue() slog.Value {
	if c == nil {
		return slog.AnyValue(nil)
	}
	attrs := []slog.Attr{
		slog.String("method", c.Method),
		slog.String("github_url", c.GitHubURL),
	}
	if c.PAT != "" {
		attrs = append(attrs, slog.String("pat", MaskedPAT(c.PAT)))
	}
	if c.GitHubApp != nil {
		attrs = append(attrs, slog.Any("github_app", c.GitHubApp))
	}
	if !c.CreatedAt.IsZero() {
		attrs = append(attrs, slog.Time("created_at", c.CreatedAt))
	}
	return slog.GroupValue(attrs...)
}

func (g *GitHubAppCreds) LogValue() slog.Value {
	if g == nil {
		return slog.AnyValue(nil)
	}
	return slog.GroupValue(
		slog.String("client_id", g.ClientID),
		slog.Int64("installation_id", g.InstallationID),
		slog.String("private_key_path", g.PrivateKeyPath),
		slog.String("account", g.Account),
	)
}

package auth

import "time"

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

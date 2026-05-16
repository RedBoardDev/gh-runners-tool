package auth

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestCredentials_LogValue_MasksPAT(t *testing.T) {
	creds := &Credentials{
		Method:    "pat",
		GitHubURL: "https://github.com/example",
		PAT:       "ghp_abcdefghijklmnopqrstuvwxyz1234567890",
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	logger.Info("loaded", "creds", creds)

	out := buf.String()
	if strings.Contains(out, "ghp_abcdefghijklmnopqrstuvwxyz1234567890") {
		t.Errorf("log output leaks raw PAT: %s", out)
	}
	if !strings.Contains(out, "ghp_") || !strings.Contains(out, "7890") {
		t.Errorf("log output should contain masked PAT excerpt, got: %s", out)
	}
}

func TestCredentials_LogValue_OmitsEmptyPAT(t *testing.T) {
	creds := &Credentials{
		Method:    "github_app",
		GitHubURL: "https://github.com/example",
		GitHubApp: &GitHubAppCreds{
			ClientID:       "Iv1.xxx",
			InstallationID: 42,
			PrivateKeyPath: "/etc/ghr/key.pem",
			Account:        "octocat",
		},
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	logger.Info("loaded", "creds", creds)

	out := buf.String()
	if strings.Contains(out, "pat=") {
		t.Errorf("empty PAT must not appear in log output: %s", out)
	}
	if !strings.Contains(out, "client_id=Iv1.xxx") {
		t.Errorf("github app fields missing from log output: %s", out)
	}
}

func TestCredentials_LogValue_NilSafe(t *testing.T) {
	var c *Credentials
	v := c.LogValue()
	if v.Kind() != slog.KindAny {
		t.Errorf("nil receiver should resolve to KindAny, got %v", v.Kind())
	}
}

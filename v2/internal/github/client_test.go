package github

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/auth"
)

func TestNewClient_PAT(t *testing.T) {
	creds := &auth.Credentials{
		Method: "pat",
		PAT:    "ghp_test1234567890",
	}

	client, err := NewClient(creds, "https://github.com/test-org")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.inner == nil {
		t.Fatal("expected non-nil inner client")
	}
}

func TestNewClient_GitHubApp(t *testing.T) {
	keyContent := `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA0Z3VS5JJcds3xfn/ygWyF8PbnGy0AHB7MhgHcTz6sE2I2yPB
aNlRtQ8aXEr55FZgMvemuafJoqfiN2OkXvMPMID2KJHnfxJPMSdMoBRk7GkLVOH
OBnG9gVmZ5A6iNFwHGO9BKnL7P7iCfxWJCFxdF0qNGBJjqMJjHb6cDAVJfb0Q5K
xHE6UKJhne1RDmaoW/4Vh+M3OAv8MXPqp0qhBkJYYlTpjRkLjF2MOqMmGKO7UmB
dVjr3HvaGRFnRlq5mzv2JlFjQFPXYiRgDrU/K3Y2MnsQfJGP7TW0j5FFsiZp7vTV
P0WBLZEQy2mvVz9y9X78JiK64ijr4EDRqKi1NQIDAQABAoIBAC5RgZ+hBx7xHNaM
pPgwGMnCd2vHsHwAaXkeAzSdRnLBDqPWJGJmaCF3B/cQHan5IMnVEL2T0KDiWqjh
ax7GiMAPPkCgarSHMC4sPXTR0NHHZxC5bED5z98rIqabSChzmZjDe6FMqpljhdJR
0K/gUVLqCRJjHNdGIFsmi2amEMGdlxEJmH3FvSmhaxAhIfxmSGNNEPzMCQl5mmFM
OqoB3BtMdn/qxg9grs08PHshqJdH6QilaRy6KfDEuHpgMZav2RI7sjChTQaI+MUN
FzkaOq1M2C17xjIT3vlQ3WJkQXZrYJP5FGGxgI2RfVROaGE8+BiFKzIGudPJ8NpB
OCSrUmECgYEA7wO6fDL+S6YJdAJ8YTBOfNny/VkECzk2sxhvP5pKGp0tzGAYq3BM
uRjdrR7Cj+cW1gi6DRezMX+r5jMXnBQkmRqyZ6u3r9XSvuEyiGmd+qNWm7iFt6FX
3VdANYsl6xMOPNmAzKm0ZFb0J9J3BHL+F+1adij6YqN+OlTIRLpbpzUCgYEA4G/W
9T1XT/dPIHr7PGBFuJ3vkLNU1ITk2LCPTCkghq9vFf+/F8RQ/eDa9fugVDJnHlMm
qiFUWHfBmoANRrAQKbw8kN6E8Oij1F5Y09mW0fqzlMF1bRUxOJ0SXdyp8RIIYO9n
g5UlD1UqRCsAWxJN7vE1VX/bZb3OIEQ0C+YfKkECgYBHPCA22lpjsJGIbgIEkk9Q
Cm1WlCXBH7SgXBMoJwJfKSIqn4TRJ9RLfMqFLVTJDNIGdIkLUJPR78VR8qJwqifz
LnGPEjMTIZEfHvJlUDI6dEe6n5ENZB9evRQ0MflIsNkGHQ0qzLGLPYGWmJ0TBy8J
aIFZ1GfwBlSPI/4ffNV8bQKBgQCFDMcMJoB+urH7sMFEgH5P3fHEQHjfJNrDaBPM
YCUWa8DTQD9/7HzIepcWKEVr4jSBK2D0B0sFqgHhD0UIc/WW7IQKyKlmEjz7oSR
7YR2FUycBRTxZ6EmGlK5E67z1Q2FHeFJgIq2ip1Rb6VLFy8yAaDPxPQ8YIBNlQdp
S+hkAQKBgQDR4LJibkXz+U/5MhQT+IhEVeEBH5fTkOD6oIOJHd17DMQ5mi+zBPf0
hB+sQ+zl3lOKJGjTTqdapnJeT8v5JD1TvVCDBii6niUoR6TFB3qxaOjv/VEL1Cf3
G5FadRKM/l54xfA+mEHxkO/nGxH7fBatEJRE3l6K9MmIq2gOMCF0MQ==
-----END RSA PRIVATE KEY-----`

	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "test-key.pem")
	if err := os.WriteFile(keyPath, []byte(keyContent), 0600); err != nil {
		t.Fatalf("write test key: %v", err)
	}

	creds := &auth.Credentials{
		Method: "github_app",
		GitHubApp: &auth.GitHubAppCreds{
			ClientID:       "Iv1.test123",
			InstallationID: 12345,
			PrivateKeyPath: keyPath,
		},
	}

	client, err := NewClient(creds, "https://github.com/test-org")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewClient_UnknownMethod(t *testing.T) {
	creds := &auth.Credentials{
		Method: "oauth",
	}

	client, err := NewClient(creds, "https://github.com/test-org")
	if err == nil {
		t.Fatal("expected error for unknown method")
	}
	if client != nil {
		t.Fatal("expected nil client on error")
	}
}

func TestNewClient_AppNilCreds(t *testing.T) {
	creds := &auth.Credentials{
		Method:    "github_app",
		GitHubApp: nil,
	}

	client, err := NewClient(creds, "https://github.com/test-org")
	if err == nil {
		t.Fatal("expected error for nil github_app creds")
	}
	if client != nil {
		t.Fatal("expected nil client on error")
	}
}

func TestNewClient_AppMissingKeyFile(t *testing.T) {
	creds := &auth.Credentials{
		Method: "github_app",
		GitHubApp: &auth.GitHubAppCreds{
			ClientID:       "Iv1.test123",
			InstallationID: 12345,
			PrivateKeyPath: "/nonexistent/path/key.pem",
		},
	}

	client, err := NewClient(creds, "https://github.com/test-org")
	if err == nil {
		t.Fatal("expected error for missing key file")
	}
	if client != nil {
		t.Fatal("expected nil client on error")
	}
}

func TestNewClient_InvalidGitHubURL(t *testing.T) {
	creds := &auth.Credentials{
		Method: "pat",
		PAT:    "ghp_test1234567890",
	}

	client, err := NewClient(creds, "://invalid-url")
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
	if client != nil {
		t.Fatal("expected nil client on error")
	}
}

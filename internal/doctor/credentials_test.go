package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCredentialsCheck_Missing(t *testing.T) {
	res := CredentialsCheck{Path: filepath.Join(t.TempDir(), "absent.json")}.Run(context.Background())
	if res.Status != StatusFail {
		t.Errorf("status = %s, want FAIL", res.Status)
	}
}

func TestCredentialsCheck_LoosePerms(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "creds.json")
	if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	res := CredentialsCheck{Path: p, Method: "pat"}.Run(context.Background())
	if res.Status != StatusWarn {
		t.Errorf("status = %s, want WARN", res.Status)
	}
}

func TestCredentialsCheck_OkPat(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "creds.json")
	if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	res := CredentialsCheck{Path: p, Method: "pat"}.Run(context.Background())
	if res.Status != StatusOK {
		t.Errorf("status = %s, want OK", res.Status)
	}
}

func TestCredentialsCheck_GitHubAppKeyLoosePerms(t *testing.T) {
	dir := t.TempDir()
	creds := filepath.Join(dir, "creds.json")
	key := filepath.Join(dir, "key.pem")
	_ = os.WriteFile(creds, []byte("{}"), 0o600)
	_ = os.WriteFile(key, []byte("---"), 0o644)
	res := CredentialsCheck{Path: creds, Method: "github_app", PrivateKeyPath: key}.Run(context.Background())
	if res.Status != StatusWarn {
		t.Errorf("status = %s, want WARN", res.Status)
	}
}

package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFilePath(t *testing.T) {
	t.Run("with GHR_CREDENTIALS_FILE env set", func(t *testing.T) {
		want := "/custom/path/credentials.json"
		t.Setenv("GHR_CREDENTIALS_FILE", want)

		got := FilePath()
		if got != want {
			t.Errorf("FilePath() = %q, want %q", got, want)
		}
	})

	t.Run("without env non-root returns home config path", func(t *testing.T) {
		t.Setenv("GHR_CREDENTIALS_FILE", "")

		got := FilePath()

		// We are running tests as a non-root user, so it should use ~/.config/ghr/credentials.json
		if os.Getuid() == 0 {
			t.Skip("test requires non-root user")
		}

		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("UserHomeDir() error: %v", err)
		}
		want := filepath.Join(home, ".config", "ghr", "credentials.json")
		if got != want {
			t.Errorf("FilePath() = %q, want %q", got, want)
		}
	})
}

func TestLoad_TokenFlag(t *testing.T) {
	// Point credentials file to a non-existent path to avoid reading real credentials
	t.Setenv("GHR_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "nonexistent.json"))
	t.Setenv("GITHUB_TOKEN", "")

	creds, source, err := Load(LoadOpts{TokenFlag: "ghp_flagtoken123"})
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if creds.Method != "pat" {
		t.Errorf("Method = %q, want %q", creds.Method, "pat")
	}
	if creds.PAT != "ghp_flagtoken123" {
		t.Errorf("PAT = %q, want %q", creds.PAT, "ghp_flagtoken123")
	}
	if source != "flag (--token)" {
		t.Errorf("source = %q, want %q", source, "flag (--token)")
	}
}

func TestLoad_EnvVar(t *testing.T) {
	// Point credentials file to a non-existent path
	t.Setenv("GHR_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "nonexistent.json"))
	t.Setenv("GITHUB_TOKEN", "ghp_envtoken456")

	creds, source, err := Load(LoadOpts{})
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if creds.Method != "pat" {
		t.Errorf("Method = %q, want %q", creds.Method, "pat")
	}
	if creds.PAT != "ghp_envtoken456" {
		t.Errorf("PAT = %q, want %q", creds.PAT, "ghp_envtoken456")
	}
	if source != "env (GITHUB_TOKEN)" {
		t.Errorf("source = %q, want %q", source, "env (GITHUB_TOKEN)")
	}
}

func TestLoad_CredentialsFile(t *testing.T) {
	dir := t.TempDir()
	credFile := filepath.Join(dir, "credentials.json")
	t.Setenv("GHR_CREDENTIALS_FILE", credFile)
	t.Setenv("GITHUB_TOKEN", "")

	creds := &Credentials{
		Method:    "pat",
		GitHubURL: "https://github.com/my-org",
		PAT:       "ghp_fromfile789",
		CreatedAt: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent() error: %v", err)
	}
	if err := os.WriteFile(credFile, data, 0600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	loaded, source, err := Load(LoadOpts{})
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.Method != "pat" {
		t.Errorf("Method = %q, want %q", loaded.Method, "pat")
	}
	if loaded.PAT != "ghp_fromfile789" {
		t.Errorf("PAT = %q, want %q", loaded.PAT, "ghp_fromfile789")
	}
	if loaded.GitHubURL != "https://github.com/my-org" {
		t.Errorf("GitHubURL = %q, want %q", loaded.GitHubURL, "https://github.com/my-org")
	}
	if !strings.Contains(source, "file") {
		t.Errorf("source = %q, want it to contain %q", source, "file")
	}
}

func TestLoad_Priority(t *testing.T) {
	t.Run("TokenFlag wins over GITHUB_TOKEN", func(t *testing.T) {
		t.Setenv("GHR_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "nonexistent.json"))
		t.Setenv("GITHUB_TOKEN", "ghp_env_should_lose")

		creds, source, err := Load(LoadOpts{TokenFlag: "ghp_flag_should_win"})
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if creds.PAT != "ghp_flag_should_win" {
			t.Errorf("PAT = %q, want %q", creds.PAT, "ghp_flag_should_win")
		}
		if source != "flag (--token)" {
			t.Errorf("source = %q, want %q", source, "flag (--token)")
		}
	})

	t.Run("GITHUB_TOKEN wins over credentials file", func(t *testing.T) {
		dir := t.TempDir()
		credFile := filepath.Join(dir, "credentials.json")
		t.Setenv("GHR_CREDENTIALS_FILE", credFile)
		t.Setenv("GITHUB_TOKEN", "ghp_env_should_win")

		fileCreds := &Credentials{
			Method:    "pat",
			PAT:       "ghp_file_should_lose",
			CreatedAt: time.Now(),
		}
		data, err := json.MarshalIndent(fileCreds, "", "  ")
		if err != nil {
			t.Fatalf("MarshalIndent() error: %v", err)
		}
		if err := os.WriteFile(credFile, data, 0600); err != nil {
			t.Fatalf("WriteFile() error: %v", err)
		}

		creds, source, err := Load(LoadOpts{})
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if creds.PAT != "ghp_env_should_win" {
			t.Errorf("PAT = %q, want %q", creds.PAT, "ghp_env_should_win")
		}
		if source != "env (GITHUB_TOKEN)" {
			t.Errorf("source = %q, want %q", source, "env (GITHUB_TOKEN)")
		}
	})
}

func TestLoad_NotAuthenticated(t *testing.T) {
	t.Setenv("GHR_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "nonexistent.json"))
	t.Setenv("GITHUB_TOKEN", "")

	_, _, err := Load(LoadOpts{})
	if err == nil {
		t.Fatal("Load() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "not authenticated")
	}
}

func TestSave_And_Load(t *testing.T) {
	dir := t.TempDir()
	credFile := filepath.Join(dir, "credentials.json")
	t.Setenv("GHR_CREDENTIALS_FILE", credFile)
	t.Setenv("GITHUB_TOKEN", "")

	original := &Credentials{
		Method:    "pat",
		GitHubURL: "https://github.com/test-org",
		PAT:       "ghp_saveandload123",
		CreatedAt: time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
	}

	if err := Save(original); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Verify file permissions are 0600
	info, err := os.Stat(credFile)
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("file permissions = %o, want %o", perm, 0600)
	}

	// Load back and verify
	loaded, source, err := Load(LoadOpts{})
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.Method != original.Method {
		t.Errorf("Method = %q, want %q", loaded.Method, original.Method)
	}
	if loaded.PAT != original.PAT {
		t.Errorf("PAT = %q, want %q", loaded.PAT, original.PAT)
	}
	if loaded.GitHubURL != original.GitHubURL {
		t.Errorf("GitHubURL = %q, want %q", loaded.GitHubURL, original.GitHubURL)
	}
	if !strings.Contains(source, "file") {
		t.Errorf("source = %q, want it to contain %q", source, "file")
	}
}

func TestLoad_WarnsOnLoosePermissions(t *testing.T) {
	dir := t.TempDir()
	credFile := filepath.Join(dir, "credentials.json")
	t.Setenv("GHR_CREDENTIALS_FILE", credFile)
	t.Setenv("GITHUB_TOKEN", "")

	creds := &Credentials{Method: "pat", PAT: "ghp_loose", GitHubURL: "https://github.com/x"}
	if err := Save(creds); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := os.Chmod(credFile, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStderr := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = origStderr }()

	if _, _, loadErr := Load(LoadOpts{}); loadErr != nil {
		t.Fatalf("Load: %v", loadErr)
	}
	w.Close()

	out, _ := io.ReadAll(r)
	if !strings.Contains(string(out), "warning") || !strings.Contains(string(out), "chmod 600") {
		t.Errorf("expected loose-perm warning, got: %q", out)
	}
}

func TestSave_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	nestedPath := filepath.Join(dir, "nested", "deep", "credentials.json")
	t.Setenv("GHR_CREDENTIALS_FILE", nestedPath)

	creds := &Credentials{
		Method:    "pat",
		PAT:       "ghp_nested123",
		CreatedAt: time.Now(),
	}

	if err := Save(creds); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Verify parent directory was created with 0700
	parentDir := filepath.Dir(nestedPath)
	info, err := os.Stat(parentDir)
	if err != nil {
		t.Fatalf("Stat(%s) error: %v", parentDir, err)
	}
	if !info.IsDir() {
		t.Errorf("%s is not a directory", parentDir)
	}
	perm := info.Mode().Perm()
	if perm != 0700 {
		t.Errorf("directory permissions = %o, want %o", perm, 0700)
	}
}

func TestSave_SetsCreatedAt(t *testing.T) {
	dir := t.TempDir()
	credFile := filepath.Join(dir, "credentials.json")
	t.Setenv("GHR_CREDENTIALS_FILE", credFile)

	before := time.Now().Add(-time.Second)

	creds := &Credentials{
		Method: "pat",
		PAT:    "ghp_timestamp123",
		// CreatedAt is zero
	}

	if err := Save(creds); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	after := time.Now().Add(time.Second)

	if creds.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should not be zero after Save()")
	}
	if creds.CreatedAt.Before(before) {
		t.Errorf("CreatedAt = %v, want after %v", creds.CreatedAt, before)
	}
	if creds.CreatedAt.After(after) {
		t.Errorf("CreatedAt = %v, want before %v", creds.CreatedAt, after)
	}
}

func TestRemove(t *testing.T) {
	t.Run("save then remove", func(t *testing.T) {
		dir := t.TempDir()
		credFile := filepath.Join(dir, "credentials.json")
		t.Setenv("GHR_CREDENTIALS_FILE", credFile)

		creds := &Credentials{
			Method:    "pat",
			PAT:       "ghp_removeme",
			CreatedAt: time.Now(),
		}
		if err := Save(creds); err != nil {
			t.Fatalf("Save() error: %v", err)
		}

		// Verify file exists
		if _, err := os.Stat(credFile); err != nil {
			t.Fatalf("file should exist before Remove(), Stat error: %v", err)
		}

		if err := Remove(); err != nil {
			t.Fatalf("Remove() error: %v", err)
		}

		// Verify file no longer exists
		if _, err := os.Stat(credFile); !os.IsNotExist(err) {
			t.Errorf("file should not exist after Remove(), Stat error: %v", err)
		}
	})

	t.Run("remove when file does not exist", func(t *testing.T) {
		t.Setenv("GHR_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "nonexistent.json"))

		if err := Remove(); err != nil {
			t.Errorf("Remove() on non-existent file should not error, got: %v", err)
		}
	})
}

func TestMaskedPAT(t *testing.T) {
	tests := []struct {
		name string
		pat  string
		want string
	}{
		{
			name: "standard PAT",
			pat:  "ghp_1234567890abcdef",
			want: "ghp_...cdef",
		},
		{
			name: "short token",
			pat:  "short",
			want: "****",
		},
		{
			name: "empty token",
			pat:  "",
			want: "****",
		},
		{
			name: "exactly 12 chars",
			pat:  "exactlytwelv",
			want: "exac...welv",
		},
		{
			name: "11 chars returns mask",
			pat:  "elevenchar!",
			want: "****",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskedPAT(tt.pat)
			if got != tt.want {
				t.Errorf("MaskedPAT(%q) = %q, want %q", tt.pat, got, tt.want)
			}
		})
	}
}

func TestValidate_PAT(t *testing.T) {
	// validatePAT hardcodes "https://api.github.com/user", so we cannot inject
	// a test server URL without modifying the production code. Instead, we test
	// validatePAT indirectly via Validate for the success case using httptest
	// by temporarily overriding http.DefaultTransport.
	t.Run("valid PAT", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify the request is well-formed
			if got := r.Header.Get("Authorization"); got != "Bearer ghp_testtoken" {
				t.Errorf("Authorization = %q, want %q", got, "Bearer ghp_testtoken")
			}
			if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
				t.Errorf("Accept = %q, want %q", got, "application/vnd.github+json")
			}
			w.Header().Set("X-OAuth-Scopes", "admin:org, repo")
			w.WriteHeader(http.StatusOK)
			resp := githubUserResponse{Login: "testuser"}
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				t.Errorf("encode response: %v", err)
			}
		}))
		defer srv.Close()

		// Override DefaultTransport to redirect api.github.com to the test server
		origTransport := http.DefaultTransport
		http.DefaultTransport = &rewriteTransport{
			targetURL: srv.URL,
			wrapped:   origTransport,
		}
		defer func() { http.DefaultTransport = origTransport }()

		result, err := Validate(context.Background(), &Credentials{
			Method: "pat",
			PAT:    "ghp_testtoken",
		})
		if err != nil {
			t.Fatalf("Validate() error: %v", err)
		}
		if !result.Valid {
			t.Error("Valid = false, want true")
		}
		if result.Username != "testuser" {
			t.Errorf("Username = %q, want %q", result.Username, "testuser")
		}
		if len(result.Scopes) != 2 {
			t.Errorf("Scopes length = %d, want 2", len(result.Scopes))
		} else {
			if result.Scopes[0] != "admin:org" {
				t.Errorf("Scopes[0] = %q, want %q", result.Scopes[0], "admin:org")
			}
			if result.Scopes[1] != "repo" {
				t.Errorf("Scopes[1] = %q, want %q", result.Scopes[1], "repo")
			}
		}
	})

	t.Run("unauthorized PAT", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, writeErr := w.Write([]byte(`{"message":"Bad credentials"}`))
			if writeErr != nil {
				t.Errorf("write response: %v", writeErr)
			}
		}))
		defer srv.Close()

		origTransport := http.DefaultTransport
		http.DefaultTransport = &rewriteTransport{
			targetURL: srv.URL,
			wrapped:   origTransport,
		}
		defer func() { http.DefaultTransport = origTransport }()

		_, err := Validate(context.Background(), &Credentials{
			Method: "pat",
			PAT:    "ghp_badtoken",
		})
		if err == nil {
			t.Fatal("Validate() expected error for unauthorized PAT, got nil")
		}
		if !strings.Contains(err.Error(), "401") {
			t.Errorf("error = %q, want it to contain %q", err.Error(), "401")
		}
	})
}

func TestValidate_GitHubApp(t *testing.T) {
	t.Run("valid private key file", func(t *testing.T) {
		dir := t.TempDir()
		keyPath := filepath.Join(dir, "test.pem")
		if err := os.WriteFile(keyPath, []byte("fake-pem-content"), 0600); err != nil {
			t.Fatalf("WriteFile() error: %v", err)
		}

		result, err := Validate(context.Background(), &Credentials{
			Method: "github_app",
			GitHubApp: &GitHubAppCreds{
				ClientID:       "Iv1.abc123",
				InstallationID: 12345678,
				PrivateKeyPath: keyPath,
			},
		})
		if err != nil {
			t.Fatalf("Validate() error: %v", err)
		}
		if !result.Valid {
			t.Error("Valid = false, want true")
		}
	})

	t.Run("non-existent private key file", func(t *testing.T) {
		_, err := Validate(context.Background(), &Credentials{
			Method: "github_app",
			GitHubApp: &GitHubAppCreds{
				ClientID:       "Iv1.abc123",
				InstallationID: 12345678,
				PrivateKeyPath: "/nonexistent/path/key.pem",
			},
		})
		if err == nil {
			t.Fatal("Validate() expected error for non-existent key, got nil")
		}
		if !strings.Contains(err.Error(), "open private key") {
			t.Errorf("error = %q, want it to contain %q", err.Error(), "open private key")
		}
	})

	t.Run("nil GitHubAppCreds", func(t *testing.T) {
		_, err := Validate(context.Background(), &Credentials{
			Method:    "github_app",
			GitHubApp: nil,
		})
		if err == nil {
			t.Fatal("Validate() expected error for nil creds, got nil")
		}
		if !strings.Contains(err.Error(), "credentials are nil") {
			t.Errorf("error = %q, want it to contain %q", err.Error(), "credentials are nil")
		}
	})
}

func TestValidate_UnknownMethod(t *testing.T) {
	_, err := Validate(context.Background(), &Credentials{
		Method: "unknown",
	})
	if err == nil {
		t.Fatal("Validate() expected error for unknown method, got nil")
	}
	if !strings.Contains(err.Error(), "unknown method") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "unknown method")
	}
}

func TestParseScopes(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   []string
	}{
		{
			name:   "multiple scopes",
			header: "admin:org, repo, workflow",
			want:   []string{"admin:org", "repo", "workflow"},
		},
		{
			name:   "single scope",
			header: "repo",
			want:   []string{"repo"},
		},
		{
			name:   "empty header",
			header: "",
			want:   nil,
		},
		{
			name:   "extra whitespace",
			header: " admin:org , repo ,  workflow ",
			want:   []string{"admin:org", "repo", "workflow"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseScopes(tt.header)
			if len(got) != len(tt.want) {
				t.Fatalf("parseScopes(%q) length = %d, want %d", tt.header, len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseScopes(%q)[%d] = %q, want %q", tt.header, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// rewriteTransport is an http.RoundTripper that redirects requests targeting
// api.github.com to a local httptest server. This allows testing validatePAT
// without modifying the production code.
type rewriteTransport struct {
	targetURL string
	wrapped   http.RoundTripper
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == "api.github.com" {
		req = req.Clone(req.Context())
		req.URL.Scheme = "http"
		parsed, err := http.NewRequest(req.Method, t.targetURL+req.URL.Path, req.Body)
		if err != nil {
			return nil, err
		}
		parsed.Header = req.Header
		req = parsed
	}
	return t.wrapped.RoundTrip(req)
}

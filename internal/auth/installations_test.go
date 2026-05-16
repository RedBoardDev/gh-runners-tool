package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListAppInstallations(t *testing.T) {
	t.Run("returns installations on 200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/app/installations" {
				t.Errorf("path = %q", r.URL.Path)
			}
			if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
				t.Errorf("Authorization = %q, want Bearer prefix", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"id": 100, "account": {"login": "akord-securite", "type": "Organization"}, "target_type": "Organization", "html_url": "https://github.com/akord-securite"},
				{"id": 200, "account": {"login": "personal", "type": "User"}, "target_type": "User", "html_url": "https://github.com/personal"}
			]`))
		}))
		defer srv.Close()

		got, err := ListAppInstallations(context.Background(), srv.URL, "fake-jwt")
		if err != nil {
			t.Fatalf("ListAppInstallations: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
		if got[0].ID != 100 || got[0].Account != "akord-securite" || got[0].AccountType != "Organization" {
			t.Errorf("got[0] = %+v", got[0])
		}
		if got[1].ID != 200 || got[1].Account != "personal" {
			t.Errorf("got[1] = %+v", got[1])
		}
	})

	t.Run("401 returns user-friendly error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"bad credentials"}`))
		}))
		defer srv.Close()

		_, err := ListAppInstallations(context.Background(), srv.URL, "wrong-jwt")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "rejected the JWT") {
			t.Errorf("error = %q, want contain 'rejected the JWT'", err)
		}
	})

	t.Run("500 returns wrapped HTTP error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("oops"))
		}))
		defer srv.Close()

		_, err := ListAppInstallations(context.Background(), srv.URL, "x")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "HTTP 500") {
			t.Errorf("error = %q, want contain 'HTTP 500'", err)
		}
	})

	t.Run("empty list returns empty slice", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`[]`))
		}))
		defer srv.Close()

		got, err := ListAppInstallations(context.Background(), srv.URL, "x")
		if err != nil {
			t.Fatalf("ListAppInstallations: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("len = %d, want 0", len(got))
		}
	})
}

func TestIssueInstallationToken(t *testing.T) {
	t.Run("returns token and permissions on 201", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/app/installations/12345/access_tokens" {
				t.Errorf("path = %q", r.URL.Path)
			}
			if r.Method != http.MethodPost {
				t.Errorf("method = %q, want POST", r.Method)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{
				"token": "ghs_abc",
				"expires_at": "2026-05-16T12:34:56Z",
				"permissions": {"administration": "write", "metadata": "read"}
			}`))
		}))
		defer srv.Close()

		got, err := IssueInstallationToken(context.Background(), srv.URL, "jwt", 12345)
		if err != nil {
			t.Fatalf("IssueInstallationToken: %v", err)
		}
		if got.Token != "ghs_abc" {
			t.Errorf("token = %q", got.Token)
		}
		if got.Permissions["administration"] != "write" {
			t.Errorf("permissions = %v", got.Permissions)
		}
	})

	t.Run("404 returns wrapped error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
		}))
		defer srv.Close()

		_, err := IssueInstallationToken(context.Background(), srv.URL, "jwt", 999)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "HTTP 404") {
			t.Errorf("error = %q", err)
		}
	})
}

func TestCheckRunnerPermissions(t *testing.T) {
	tests := []struct {
		name    string
		perms   map[string]string
		wantErr bool
	}{
		{"administration:write passes", map[string]string{"administration": "write"}, false},
		{"administration:admin passes", map[string]string{"administration": "admin"}, false},
		{"org runners write passes", map[string]string{"organization_self_hosted_runners": "write"}, false},
		{"administration:read fails", map[string]string{"administration": "read"}, true},
		{"only metadata fails", map[string]string{"metadata": "read"}, true},
		{"empty fails", map[string]string{}, true},
		{"nil fails", nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckRunnerPermissions(tc.perms)
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestAPIBaseURL(t *testing.T) {
	tests := []struct {
		name      string
		githubURL string
		want      string
		wantErr   bool
	}{
		{"empty defaults to github.com API", "", "https://api.github.com", false},
		{"github.com org", "https://github.com/akord-securite", "https://api.github.com", false},
		{"github.com repo", "https://github.com/akord-securite/repo", "https://api.github.com", false},
		{"GHES org", "https://ghe.corp.example/myorg", "https://ghe.corp.example/api/v3", false},
		{"invalid URL", "://broken", "", true},
		{"missing host", "noscheme", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := APIBaseURL(tc.githubURL)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for %q, got nil (result=%q)", tc.githubURL, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("APIBaseURL(%q): %v", tc.githubURL, err)
			}
			if got != tc.want {
				t.Errorf("APIBaseURL(%q) = %q, want %q", tc.githubURL, got, tc.want)
			}
		})
	}
}

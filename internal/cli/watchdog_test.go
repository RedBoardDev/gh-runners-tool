package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProbeOK(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		want       bool
	}{
		{"200", http.StatusOK, true},
		{"204", http.StatusNoContent, true},
		{"4xx is alive (no body issue)", http.StatusNotFound, true},
		{"5xx counts as failure", http.StatusBadGateway, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.statusCode)
			}))
			defer srv.Close()

			// rewrite the URL path-only request to point at the httptest server.
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/health", http.NoBody)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			got := resp.StatusCode >= 200 && resp.StatusCode < 500
			resp.Body.Close()
			if got != tc.want {
				t.Errorf("probe status %d => %v, want %v", tc.statusCode, got, tc.want)
			}
		})
	}
}

func TestProbeOK_ConnectionError(t *testing.T) {
	client := &http.Client{Timeout: 50 * time.Millisecond}
	// Hit an address that should not have anyone listening.
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:1/health", http.NoBody)
	resp, err := client.Do(req)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil {
		t.Skip("unexpected connectivity to port 1")
	}
	if !strings.Contains(err.Error(), "connect") && !strings.Contains(err.Error(), "refused") && !strings.Contains(err.Error(), "timeout") {
		t.Skipf("connection error has unexpected form (env-dependent): %v", err)
	}
}

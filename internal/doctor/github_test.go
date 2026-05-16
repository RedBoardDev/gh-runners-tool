package doctor

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGitHubAPICheck_NoTokenSkips(t *testing.T) {
	res := GitHubAPICheck{}.Run(context.Background())
	if res.Status != StatusSkip {
		t.Errorf("status = %s, want SKIP", res.Status)
	}
}

func TestGitHubAPICheck_AuthRejectedIsFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	res := GitHubAPICheck{BaseURL: srv.URL, Token: "tok"}.Run(context.Background())
	if res.Status != StatusFail {
		t.Errorf("status = %s, want FAIL", res.Status)
	}
}

func TestGitHubAPICheck_HealthyRateLimit(t *testing.T) {
	reset := time.Now().Add(30 * time.Minute).Unix()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"resources":{"core":{"limit":5000,"remaining":4000,"reset":%d}}}`, reset)
	}))
	defer srv.Close()
	res := GitHubAPICheck{BaseURL: srv.URL, Token: "tok"}.Run(context.Background())
	if res.Status != StatusOK {
		t.Errorf("status = %s, want OK", res.Status)
	}
}

func TestGitHubAPICheck_LowRateLimitIsWarn(t *testing.T) {
	reset := time.Now().Add(30 * time.Minute).Unix()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"resources":{"core":{"limit":5000,"remaining":500,"reset":%d}}}`, reset)
	}))
	defer srv.Close()
	res := GitHubAPICheck{BaseURL: srv.URL, Token: "tok"}.Run(context.Background())
	if res.Status != StatusWarn {
		t.Errorf("status = %s, want WARN", res.Status)
	}
}

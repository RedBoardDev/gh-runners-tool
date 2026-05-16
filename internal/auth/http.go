package auth

import (
	"io"
	"net/http"
	"time"
)

const (
	httpTimeout    = 30 * time.Second
	maxBodyExcerpt = 500
)

var httpClient = &http.Client{Timeout: httpTimeout}

func drainBody(resp *http.Response) {
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

func truncateBody(s string) string {
	if len(s) > maxBodyExcerpt {
		return s[:maxBodyExcerpt] + "..."
	}
	return s
}

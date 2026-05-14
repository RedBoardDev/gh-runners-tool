package monitoring

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type UptimeKumaConfig struct {
	BaseURL            string
	DaemonToken        string
	GroupTokens        map[string]string
	DegradedThreshold  float64
	ReportHealthAsPing bool
}

type UptimeKuma struct {
	cfg    UptimeKumaConfig
	client *http.Client
	logger *slog.Logger
}

func NewUptimeKuma(cfg UptimeKumaConfig, logger *slog.Logger) *UptimeKuma {
	return &UptimeKuma{
		cfg: cfg,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

func (u *UptimeKuma) ReportDaemonHealth(ctx context.Context, groups, totalActual, totalDesired int, checkDuration time.Duration) {
	if u.cfg.DaemonToken == "" {
		return
	}

	msg := fmt.Sprintf("groups=%d runners=%d/%d", groups, totalActual, totalDesired)
	ping := float64(checkDuration.Milliseconds())

	pushErr := u.push(ctx, u.cfg.DaemonToken, "up", msg, ping)
	if pushErr != nil {
		u.logger.Warn("uptime-kuma daemon push failed", "error", pushErr)
	}
}

func (u *UptimeKuma) ReportGroupHealth(ctx context.Context, group string, actual, desired int) {
	token, ok := u.cfg.GroupTokens[group]
	if !ok || token == "" {
		return
	}

	status, msg := groupStatus(actual, desired, u.cfg.DegradedThreshold)
	ping := -1.0
	if u.cfg.ReportHealthAsPing && desired > 0 {
		ping = (float64(actual) / float64(desired)) * 100
	}

	pushErr := u.push(ctx, token, status, msg, ping)
	if pushErr != nil {
		u.logger.Warn("uptime-kuma group push failed", "group", group, "error", pushErr)
	}
}

func (u *UptimeKuma) push(ctx context.Context, token, status, msg string, ping float64) error {
	baseURL := strings.TrimRight(u.cfg.BaseURL, "/")
	pushURL := fmt.Sprintf("%s/api/push/%s?status=%s&msg=%s",
		baseURL, token, status, url.QueryEscape(truncateMsg(msg, 250)))

	if ping >= 0 {
		pushURL += fmt.Sprintf("&ping=%.1f", ping)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pushURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("create push request: %w", err)
	}

	resp, err := u.client.Do(req)
	if err != nil {
		return fmt.Errorf("push request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("push failed: HTTP %d", resp.StatusCode)
	}
	return nil
}

func groupStatus(actual, desired int, threshold float64) (status, msg string) {
	if desired == 0 {
		return "up", "idle (0 desired)"
	}
	if actual == 0 {
		return "down", fmt.Sprintf("0/%d runners (outage)", desired)
	}

	ratio := float64(actual) / float64(desired)
	if ratio < threshold {
		return "down", fmt.Sprintf("%d/%d runners (critical)", actual, desired)
	}
	if actual < desired {
		return "up", fmt.Sprintf("%d/%d runners (degraded)", actual, desired)
	}
	return "up", fmt.Sprintf("%d/%d runners", actual, desired)
}

func truncateMsg(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

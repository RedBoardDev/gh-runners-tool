package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
)

const discordMinInterval = 400 * time.Millisecond

// discordServerErrorBackoff is the wait between a 5xx response and the retry.
// Overridable from tests via the test-only helper in discord_test_helper.go.
var discordServerErrorBackoff = 2 * time.Second

type DiscordConfig struct {
	WebhookURL string
	Username   string
	AvatarURL  string
	Mentions   DiscordMentions
}

type DiscordMentions struct {
	Error    string
	Critical string
}

type DiscordProvider struct {
	cfg      DiscordConfig
	client   *http.Client
	mu       sync.Mutex
	lastSend time.Time
}

func NewDiscord(cfg *DiscordConfig) *DiscordProvider {
	return &DiscordProvider{
		cfg:    *cfg,
		client: &http.Client{},
	}
}

func (d *DiscordProvider) Name() string { return "discord" }

func (d *DiscordProvider) Send(ctx context.Context, event *model.Event) error {
	d.throttle()

	payload := d.buildPayload(event)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal discord payload: %w", err)
	}

	resp, err := d.postWithRateLimitRetry(ctx, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord webhook returned status %d", resp.StatusCode)
	}

	return nil
}

func (d *DiscordProvider) postWithRateLimitRetry(ctx context.Context, body []byte) (*http.Response, error) {
	resp, err := d.doPost(ctx, body)
	if err != nil {
		return nil, err
	}

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("discord rate limited, context canceled: %w", ctx.Err())
		case <-time.After(retryAfter):
		}
		return d.doPost(ctx, body)

	case resp.StatusCode >= 500:
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("discord 5xx, context canceled: %w", ctx.Err())
		case <-time.After(discordServerErrorBackoff):
		}
		return d.doPost(ctx, body)
	}

	return resp, nil
}

func (d *DiscordProvider) throttle() {
	d.mu.Lock()
	defer d.mu.Unlock()

	elapsed := time.Since(d.lastSend)
	if elapsed < discordMinInterval {
		time.Sleep(discordMinInterval - elapsed)
	}
	d.lastSend = time.Now()
}

func (d *DiscordProvider) doPost(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create discord request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send discord webhook: %w", err)
	}
	return resp, nil
}

func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return time.Second
	}
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return time.Second
	}
	return time.Duration(seconds * float64(time.Second))
}

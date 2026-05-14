package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
)

type WebhookConfig struct {
	URL     string
	Method  string
	Headers map[string]string
}

type WebhookProvider struct {
	cfg    WebhookConfig
	client *http.Client
}

func NewWebhook(cfg WebhookConfig) *WebhookProvider {
	method := cfg.Method
	if method == "" {
		method = http.MethodPost
	}
	return &WebhookProvider{
		cfg: WebhookConfig{
			URL:     cfg.URL,
			Method:  method,
			Headers: cfg.Headers,
		},
		client: &http.Client{},
	}
}

func (w *WebhookProvider) Name() string { return "webhook" }

func (w *WebhookProvider) Send(ctx context.Context, event *model.Event) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, w.cfg.Method, w.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}

	for k, v := range w.cfg.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
)

type DiscordConfig struct {
	WebhookURL string
	Username   string
	Mentions   DiscordMentions
}

type DiscordMentions struct {
	Error    string
	Critical string
}

type DiscordProvider struct {
	cfg    DiscordConfig
	client *http.Client
}

func NewDiscord(cfg DiscordConfig) *DiscordProvider {
	return &DiscordProvider{
		cfg:    cfg,
		client: &http.Client{},
	}
}

func (d *DiscordProvider) Name() string { return "discord" }

func (d *DiscordProvider) Send(ctx context.Context, event model.Event) error {
	payload := d.buildPayload(event)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal discord payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create discord request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("send discord webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("discord rate limited (429)")
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord webhook returned status %d", resp.StatusCode)
	}

	return nil
}

func (d *DiscordProvider) buildPayload(event model.Event) discordPayload {
	fields := d.buildFields(event)
	embed := discordEmbed{
		Title:       event.Type,
		Description: event.Message,
		Color:       colorForLevel(event.Level),
		Fields:      fields,
		Timestamp:   event.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
	}

	payload := discordPayload{
		Username: d.cfg.Username,
		Embeds:   []discordEmbed{embed},
	}

	mention := d.mentionForLevel(event.Level)
	if mention != "" {
		payload.Content = mention
	}

	return payload
}

func (d *DiscordProvider) buildFields(event model.Event) []discordField {
	var fields []discordField

	if event.Group != "" {
		fields = append(fields, discordField{Name: "Group", Value: event.Group, Inline: true})
	}
	if event.Runner != "" {
		fields = append(fields, discordField{Name: "Runner", Value: event.Runner, Inline: true})
	}

	keys := make([]string, 0, len(event.Details))
	for k := range event.Details {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		fields = append(fields, discordField{Name: k, Value: event.Details[k], Inline: false})
	}

	return fields
}

func (d *DiscordProvider) mentionForLevel(level model.EventLevel) string {
	switch level {
	case model.LevelError:
		return d.cfg.Mentions.Error
	case model.LevelCritical:
		return d.cfg.Mentions.Critical
	default:
		return ""
	}
}

func colorForLevel(level model.EventLevel) int {
	switch level {
	case model.LevelInfo:
		return 0x3498DB
	case model.LevelWarning:
		return 0xF39C12
	case model.LevelError:
		return 0xE74C3C
	case model.LevelCritical:
		return 0x992D22
	default:
		return 0x3498DB
	}
}

type discordPayload struct {
	Username string         `json:"username,omitempty"`
	Content  string         `json:"content,omitempty"`
	Embeds   []discordEmbed `json:"embeds"`
}

type discordEmbed struct {
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Color       int            `json:"color"`
	Fields      []discordField `json:"fields,omitempty"`
	Timestamp   string         `json:"timestamp,omitempty"`
}

type discordField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

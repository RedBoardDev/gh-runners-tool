package notification

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
)

func TestDiscordProvider_Name(t *testing.T) {
	d := NewDiscord(DiscordConfig{})
	if d.Name() != "discord" {
		t.Errorf("Name() = %q, want %q", d.Name(), "discord")
	}
}

func TestDiscordProvider_Send(t *testing.T) {
	baseEvent := model.Event{
		Type:      "health.zombie_runner",
		Level:     model.LevelError,
		Group:     "backend",
		Runner:    "runner-abc",
		Message:   "Zombie runner detected",
		Details:   map[string]string{"pid": "12345", "action": "killed"},
		Timestamp: time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC),
	}

	t.Run("sends valid payload", func(t *testing.T) {
		var received discordPayload

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("method = %s, want POST", r.Method)
			}
			if ct := r.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		d := NewDiscord(DiscordConfig{
			WebhookURL: srv.URL,
			Username:   "ghr-test",
			Mentions:   DiscordMentions{Error: "<@&123>"},
		})

		err := d.Send(context.Background(), baseEvent)
		if err != nil {
			t.Fatalf("Send() error = %v", err)
		}

		if received.Username != "ghr-test" {
			t.Errorf("username = %q, want %q", received.Username, "ghr-test")
		}
		if received.Content != "<@&123>" {
			t.Errorf("content = %q, want %q", received.Content, "<@&123>")
		}
		if len(received.Embeds) != 1 {
			t.Fatalf("len(embeds) = %d, want 1", len(received.Embeds))
		}
		embed := received.Embeds[0]
		if embed.Title != "health.zombie_runner" {
			t.Errorf("title = %q, want %q", embed.Title, "health.zombie_runner")
		}
		if embed.Description != "Zombie runner detected" {
			t.Errorf("description = %q, want %q", embed.Description, "Zombie runner detected")
		}
		if embed.Color != 0xE74C3C {
			t.Errorf("color = %d, want %d", embed.Color, 0xE74C3C)
		}
		if embed.Timestamp != "2025-01-15T14:30:00Z" {
			t.Errorf("timestamp = %q, want %q", embed.Timestamp, "2025-01-15T14:30:00Z")
		}
	})

	t.Run("includes group and runner fields", func(t *testing.T) {
		var received discordPayload

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		d := NewDiscord(DiscordConfig{WebhookURL: srv.URL})
		if err := d.Send(context.Background(), baseEvent); err != nil {
			t.Fatalf("Send() error = %v", err)
		}

		fields := received.Embeds[0].Fields
		if len(fields) < 2 {
			t.Fatalf("got %d fields, want at least 2", len(fields))
		}
		if fields[0].Name != "Group" || fields[0].Value != "backend" {
			t.Errorf("field[0] = %v, want Group=backend", fields[0])
		}
		if fields[1].Name != "Runner" || fields[1].Value != "runner-abc" {
			t.Errorf("field[1] = %v, want Runner=runner-abc", fields[1])
		}
	})

	t.Run("no mention for info level", func(t *testing.T) {
		var received discordPayload

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		d := NewDiscord(DiscordConfig{
			WebhookURL: srv.URL,
			Mentions:   DiscordMentions{Error: "<@&123>", Critical: "@everyone"},
		})

		infoEvent := baseEvent
		infoEvent.Level = model.LevelInfo

		if err := d.Send(context.Background(), infoEvent); err != nil {
			t.Fatalf("Send() error = %v", err)
		}

		if received.Content != "" {
			t.Errorf("content = %q, want empty for info level", received.Content)
		}
	})

	t.Run("critical mention", func(t *testing.T) {
		var received discordPayload

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		d := NewDiscord(DiscordConfig{
			WebhookURL: srv.URL,
			Mentions:   DiscordMentions{Critical: "@everyone"},
		})

		critEvent := baseEvent
		critEvent.Level = model.LevelCritical

		if err := d.Send(context.Background(), critEvent); err != nil {
			t.Fatalf("Send() error = %v", err)
		}

		if received.Content != "@everyone" {
			t.Errorf("content = %q, want @everyone", received.Content)
		}
	})

	t.Run("rate limit returns error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer srv.Close()

		d := NewDiscord(DiscordConfig{WebhookURL: srv.URL})
		err := d.Send(context.Background(), baseEvent)
		if err == nil {
			t.Fatal("expected error for 429")
		}
	})

	t.Run("non-2xx returns error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		d := NewDiscord(DiscordConfig{WebhookURL: srv.URL})
		err := d.Send(context.Background(), baseEvent)
		if err == nil {
			t.Fatal("expected error for 500")
		}
	})

	t.Run("empty group and runner omits those fields", func(t *testing.T) {
		var received discordPayload

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		d := NewDiscord(DiscordConfig{WebhookURL: srv.URL})
		evt := model.Event{
			Type:      "daemon.start",
			Level:     model.LevelInfo,
			Message:   "started",
			Timestamp: time.Now(),
		}

		if err := d.Send(context.Background(), evt); err != nil {
			t.Fatalf("Send() error = %v", err)
		}

		for _, f := range received.Embeds[0].Fields {
			if f.Name == "Group" || f.Name == "Runner" {
				t.Errorf("unexpected field %q for empty group/runner", f.Name)
			}
		}
	})
}

func TestColorForLevel(t *testing.T) {
	tests := []struct {
		level model.EventLevel
		want  int
	}{
		{model.LevelInfo, 0x3498DB},
		{model.LevelWarning, 0xF39C12},
		{model.LevelError, 0xE74C3C},
		{model.LevelCritical, 0x992D22},
		{model.EventLevel("unknown"), 0x3498DB},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			got := colorForLevel(tt.level)
			if got != tt.want {
				t.Errorf("colorForLevel(%q) = %d, want %d", tt.level, got, tt.want)
			}
		})
	}
}

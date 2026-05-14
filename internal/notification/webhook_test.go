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

func TestWebhookProvider_Name(t *testing.T) {
	w := NewWebhook(WebhookConfig{})
	if w.Name() != "webhook" {
		t.Errorf("Name() = %q, want %q", w.Name(), "webhook")
	}
}

func TestWebhookProvider_Send(t *testing.T) {
	baseEvent := model.Event{
		Type:      "runner.started",
		Level:     model.LevelInfo,
		Group:     "ci",
		Runner:    "runner-x1",
		Message:   "Runner started",
		Details:   map[string]string{"version": "2.320"},
		Timestamp: time.Date(2025, 3, 10, 8, 0, 0, 0, time.UTC),
	}

	t.Run("sends JSON payload with POST", func(t *testing.T) {
		var received model.Event

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
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		wp := NewWebhook(WebhookConfig{URL: srv.URL})
		err := wp.Send(context.Background(), baseEvent)
		if err != nil {
			t.Fatalf("Send() error = %v", err)
		}

		if received.Type != "runner.started" {
			t.Errorf("type = %q, want %q", received.Type, "runner.started")
		}
		if received.Message != "Runner started" {
			t.Errorf("message = %q, want %q", received.Message, "Runner started")
		}
	})

	t.Run("uses configured method", func(t *testing.T) {
		var gotMethod string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		wp := NewWebhook(WebhookConfig{URL: srv.URL, Method: http.MethodPut})
		if err := wp.Send(context.Background(), baseEvent); err != nil {
			t.Fatalf("Send() error = %v", err)
		}

		if gotMethod != http.MethodPut {
			t.Errorf("method = %s, want PUT", gotMethod)
		}
	})

	t.Run("sets configured headers", func(t *testing.T) {
		var gotAuth string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		wp := NewWebhook(WebhookConfig{
			URL:     srv.URL,
			Headers: map[string]string{"Authorization": "Bearer tok123"},
		})

		if err := wp.Send(context.Background(), baseEvent); err != nil {
			t.Fatalf("Send() error = %v", err)
		}

		if gotAuth != "Bearer tok123" {
			t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer tok123")
		}
	})

	t.Run("non-2xx returns error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer srv.Close()

		wp := NewWebhook(WebhookConfig{URL: srv.URL})
		err := wp.Send(context.Background(), baseEvent)
		if err == nil {
			t.Fatal("expected error for 403")
		}
	})

	t.Run("defaults to POST when method empty", func(t *testing.T) {
		var gotMethod string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		wp := NewWebhook(WebhookConfig{URL: srv.URL, Method: ""})
		if err := wp.Send(context.Background(), baseEvent); err != nil {
			t.Fatalf("Send() error = %v", err)
		}

		if gotMethod != http.MethodPost {
			t.Errorf("method = %s, want POST", gotMethod)
		}
	})

	t.Run("connection error returns wrapped error", func(t *testing.T) {
		wp := NewWebhook(WebhookConfig{URL: "http://127.0.0.1:1"})
		err := wp.Send(context.Background(), baseEvent)
		if err == nil {
			t.Fatal("expected error for unreachable host")
		}
	})
}

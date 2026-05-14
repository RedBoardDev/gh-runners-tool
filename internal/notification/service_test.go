package notification

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
)

type fakeProvider struct {
	name   string
	mu     sync.Mutex
	events []model.Event
	err    error
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Send(_ context.Context, event model.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
	return f.err
}

func (f *fakeProvider) received() []model.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]model.Event, len(f.events))
	copy(cp, f.events)
	return cp
}

func TestService_Notify(t *testing.T) {
	event := model.Event{
		Type:      "daemon.start",
		Level:     model.LevelInfo,
		Message:   "started",
		Timestamp: time.Now(),
	}

	t.Run("sends to all matching providers", func(t *testing.T) {
		p1 := &fakeProvider{name: "p1"}
		p2 := &fakeProvider{name: "p2"}

		svc := New(
			[]Provider{p1, p2},
			map[string]EventFilter{},
			slog.Default(),
		)

		svc.Notify(context.Background(), event)

		if len(p1.received()) != 1 {
			t.Errorf("p1 got %d events, want 1", len(p1.received()))
		}
		if len(p2.received()) != 1 {
			t.Errorf("p2 got %d events, want 1", len(p2.received()))
		}
	})

	t.Run("filters events per provider", func(t *testing.T) {
		p1 := &fakeProvider{name: "p1"}
		p2 := &fakeProvider{name: "p2"}

		svc := New(
			[]Provider{p1, p2},
			map[string]EventFilter{
				"p1": {Patterns: []string{"daemon.*"}},
				"p2": {Patterns: []string{"health.*"}},
			},
			slog.Default(),
		)

		svc.Notify(context.Background(), event)

		if len(p1.received()) != 1 {
			t.Errorf("p1 got %d events, want 1", len(p1.received()))
		}
		if len(p2.received()) != 0 {
			t.Errorf("p2 got %d events, want 0", len(p2.received()))
		}
	})

	t.Run("no filter means all events", func(t *testing.T) {
		p := &fakeProvider{name: "p1"}

		svc := New(
			[]Provider{p},
			map[string]EventFilter{},
			slog.Default(),
		)

		svc.Notify(context.Background(), event)

		if len(p.received()) != 1 {
			t.Errorf("got %d events, want 1", len(p.received()))
		}
	})

	t.Run("provider error is logged not propagated", func(t *testing.T) {
		p := &fakeProvider{name: "failing", err: errors.New("connection refused")}

		svc := New(
			[]Provider{p},
			map[string]EventFilter{},
			slog.Default(),
		)

		svc.Notify(context.Background(), event)

		if len(p.received()) != 1 {
			t.Errorf("got %d events, want 1", len(p.received()))
		}
	})

	t.Run("continues to next provider after error", func(t *testing.T) {
		p1 := &fakeProvider{name: "fail", err: errors.New("boom")}
		p2 := &fakeProvider{name: "ok"}

		svc := New(
			[]Provider{p1, p2},
			map[string]EventFilter{},
			slog.Default(),
		)

		svc.Notify(context.Background(), event)

		if len(p2.received()) != 1 {
			t.Errorf("p2 got %d events, want 1", len(p2.received()))
		}
	})

	t.Run("no providers does not panic", func(t *testing.T) {
		svc := New(nil, nil, slog.Default())
		svc.Notify(context.Background(), event)
	})
}

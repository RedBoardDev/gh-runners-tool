package notification

import (
	"context"
	"log/slog"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
)

type Provider interface {
	Name() string
	Send(ctx context.Context, event *model.Event) error
}

type Service struct {
	logger    *slog.Logger
	providers []providerEntry
}

type providerEntry struct {
	provider Provider
	filter   EventFilter
}

func New(providers []Provider, filters map[string]EventFilter, logger *slog.Logger) *Service {
	entries := make([]providerEntry, 0, len(providers))
	for _, p := range providers {
		f := filters[p.Name()]
		entries = append(entries, providerEntry{
			provider: p,
			filter:   f,
		})
	}
	return &Service{
		logger:    logger,
		providers: entries,
	}
}

func (s *Service) Notify(ctx context.Context, event *model.Event) {
	for _, entry := range s.providers {
		if !entry.filter.Matches(event.Type, string(event.Level)) {
			continue
		}

		if err := entry.provider.Send(ctx, event); err != nil {
			s.logger.Warn("notification send failed",
				"provider", entry.provider.Name(),
				"event", event.Type,
				"error", err,
			)
		}
	}
}

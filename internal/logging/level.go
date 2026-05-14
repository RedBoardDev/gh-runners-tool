package logging

import (
	"log/slog"
	"strings"
)

type LogConfig struct {
	Level         string
	Format        string
	Dir           string
	RetentionDays int
	RunnerOutput  bool
}

func ParseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

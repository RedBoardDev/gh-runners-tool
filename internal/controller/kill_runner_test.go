package controller

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/runner"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

func TestKillRunner_GroupNotFound(t *testing.T) {
	c := &GroupController{
		scalers: make(map[string]*MacOSScaler),
		logger:  testLogger(),
	}

	err := c.KillRunner(context.Background(), "missing-group", "r1")
	if err == nil {
		t.Fatal("expected error for missing group")
	}
}

func TestKillRunner_RunnerNotFound(t *testing.T) {
	scaler := &MacOSScaler{
		groupName: "group-a",
		idle:      make(map[string]*runner.Process),
		busy:      make(map[string]*runner.Process),
		logger:    testLogger(),
	}

	c := &GroupController{
		scalers: map[string]*MacOSScaler{"group-a": scaler},
		logger:  testLogger(),
	}

	err := c.KillRunner(context.Background(), "group-a", "r-nonexistent")
	if err == nil {
		t.Fatal("expected error for missing runner")
	}
}

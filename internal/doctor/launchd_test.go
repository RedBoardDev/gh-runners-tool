package doctor

import (
	"context"
	"path/filepath"
	"testing"
)

func TestLaunchdCheck_NotInstalledHintUsesExistingStartCommand(t *testing.T) {
	check := LaunchdCheck{
		Label:     "com.ghr.test",
		PlistPath: filepath.Join(t.TempDir(), "missing.plist"),
	}

	result := check.Run(context.Background())

	if result.Status != StatusWarn {
		t.Fatalf("status = %s, want %s", result.Status, StatusWarn)
	}
	if result.Hint != "run 'ghr start' to register the launchd service" {
		t.Fatalf("hint = %q", result.Hint)
	}
}

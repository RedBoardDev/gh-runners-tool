package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCacheCheck_EmptyPathIsSkip(t *testing.T) {
	res := CacheCheck{}.Run(context.Background())
	if res.Status != StatusSkip {
		t.Errorf("status = %s, want SKIP", res.Status)
	}
}

func TestCacheCheck_MissingDirIsSkip(t *testing.T) {
	res := CacheCheck{Path: filepath.Join(t.TempDir(), "absent")}.Run(context.Background())
	if res.Status != StatusSkip {
		t.Errorf("status = %s, want SKIP", res.Status)
	}
}

func TestCacheCheck_ReportsSize(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "blob"), []byte("payload"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	res := CacheCheck{Path: dir}.Run(context.Background())
	if res.Status != StatusOK {
		t.Errorf("status = %s, want OK", res.Status)
	}
	if res.Summary == "" {
		t.Errorf("summary empty, want size info")
	}
}

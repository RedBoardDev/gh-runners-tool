package doctor

import (
	"context"
	"math"
	"testing"
)

func TestDiskCheck_NoPathsIsSkip(t *testing.T) {
	res := DiskCheck{}.Run(context.Background())
	if res.Status != StatusSkip {
		t.Errorf("status = %s, want SKIP", res.Status)
	}
}

func TestDiskCheck_TempDirIsOK(t *testing.T) {
	res := DiskCheck{Paths: []string{t.TempDir()}, MinFree: 1024}.Run(context.Background())
	if res.Status != StatusOK {
		t.Errorf("status = %s, want OK", res.Status)
	}
}

func TestDiskCheck_ImpossibleMinimumIsFail(t *testing.T) {
	res := DiskCheck{Paths: []string{t.TempDir()}, MinFree: math.MaxInt64}.Run(context.Background())
	if res.Status != StatusFail {
		t.Errorf("status = %s, want FAIL", res.Status)
	}
}

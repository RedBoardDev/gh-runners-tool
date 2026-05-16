package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type RunnerCheck struct {
	CacheDir string
}

func (c RunnerCheck) Name() string { return "runner" }

func (c RunnerCheck) Run(_ context.Context) Result {
	res := Result{Name: c.Name(), Details: []string{c.CacheDir}}

	if c.CacheDir == "" {
		res.Status = StatusFail
		res.Summary = "runner cache dir not configured"
		res.Hint = "set runner.cache_dir in the config file"
		return res
	}

	info, err := os.Stat(c.CacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			res.Status = StatusWarn
			res.Summary = "cache dir absent, will be created on first start"
			res.Hint = "no action needed; created automatically by the daemon"
			return res
		}
		res.Status = StatusFail
		res.Summary = fmt.Sprintf("stat cache dir: %v", err)
		return res
	}
	if !info.IsDir() {
		res.Status = StatusFail
		res.Summary = "cache path exists but is not a directory"
		return res
	}

	testFile := filepath.Join(c.CacheDir, ".ghr-doctor-write-test")
	if err := os.WriteFile(testFile, []byte{}, 0o600); err != nil {
		res.Status = StatusFail
		res.Summary = fmt.Sprintf("cache dir not writable: %v", err)
		res.Hint = "fix ownership or perms on the cache dir"
		return res
	}
	_ = os.Remove(testFile)

	versions, err := listRunnerVersions(c.CacheDir)
	if err != nil {
		res.Status = StatusFail
		res.Summary = fmt.Sprintf("read cache dir: %v", err)
		return res
	}
	if len(versions) == 0 {
		res.Status = StatusWarn
		res.Summary = "no runner binary cached"
		res.Hint = "first 'ghr start' will download the runner; this is expected on fresh installs"
		return res
	}

	res.Status = StatusOK
	res.Summary = fmt.Sprintf("%d runner version(s) cached", len(versions))
	res.Details = append(res.Details, "versions: "+strings.Join(versions, ", "))
	return res
}

func listRunnerVersions(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var versions []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(dir, name, ".complete")); statErr != nil {
			continue
		}
		versions = append(versions, name)
	}
	return versions, nil
}

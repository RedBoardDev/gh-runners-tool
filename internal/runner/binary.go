package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/logging"
)

type BinaryManager struct {
	cacheDir   string
	logger     *slog.Logger
	httpClient *http.Client
	locks      sync.Map
}

func NewBinaryManager(cacheDir string, logger *slog.Logger) *BinaryManager {
	return &BinaryManager{
		cacheDir: cacheDir,
		logger:   logger,
		httpClient: &http.Client{
			Timeout: 10 * time.Minute,
		},
	}
}

func (m *BinaryManager) EnsureBits(ctx context.Context, version string) (string, error) {
	resolved := version
	if resolved == "latest" {
		v, err := m.resolveLatestVersion(ctx)
		if err != nil {
			return "", fmt.Errorf("resolve latest runner version: %w", err)
		}
		resolved = v
		m.logger.InfoContext(ctx, "resolved latest runner version", logging.KeyVersion, resolved)
	}

	mu := m.lockFor(resolved)
	mu.Lock()
	defer mu.Unlock()

	destDir := filepath.Join(m.cacheDir, resolved)
	marker := filepath.Join(destDir, ".complete")

	if _, err := os.Stat(marker); err == nil {
		m.logger.DebugContext(ctx, "runner binary cached", logging.KeyVersion, resolved, logging.KeyPath, destDir)
		return destDir, nil
	}

	if _, err := os.Stat(destDir); err == nil {
		m.logger.WarnContext(ctx, "removing incomplete runner cache", logging.KeyVersion, resolved, logging.KeyPath, destDir)
		if rmErr := os.RemoveAll(destDir); rmErr != nil {
			return "", fmt.Errorf("clean stale cache %s: %w", destDir, rmErr)
		}
	}

	m.logger.InfoContext(ctx, "downloading runner binary", logging.KeyVersion, resolved, logging.KeyPath, destDir)

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("create cache dir %s: %w", destDir, err)
	}

	if err := downloadAndExtract(ctx, m.httpClient, m.logger, resolved, destDir); err != nil {
		if rmErr := os.RemoveAll(destDir); rmErr != nil {
			m.logger.WarnContext(ctx, "failed to clean partial download", logging.KeyPath, destDir, logging.KeyError, rmErr)
		}
		return "", fmt.Errorf("download runner %s: %w", resolved, err)
	}

	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		if rmErr := os.RemoveAll(destDir); rmErr != nil {
			m.logger.WarnContext(ctx, "failed to clean cache after marker write", logging.KeyPath, destDir, logging.KeyError, rmErr)
		}
		return "", fmt.Errorf("write completion marker %s: %w", marker, err)
	}

	m.logger.InfoContext(ctx, "runner binary ready", logging.KeyVersion, resolved, logging.KeyPath, destDir)

	if err := m.gcOldVersions(ctx, resolved); err != nil {
		m.logger.WarnContext(ctx, "cache GC failed", logging.KeyError, err)
	}

	return destDir, nil
}

const cacheKeepVersions = 3

func (m *BinaryManager) gcOldVersions(ctx context.Context, keep string) error {
	entries, err := os.ReadDir(m.cacheDir)
	if err != nil {
		return fmt.Errorf("read cache dir: %w", err)
	}

	type cacheEntry struct {
		name    string
		modTime time.Time
	}

	completed := make([]cacheEntry, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || e.Name() == keep {
			continue
		}
		marker := filepath.Join(m.cacheDir, e.Name(), ".complete")
		info, statErr := os.Stat(marker)
		if statErr != nil {
			continue
		}
		completed = append(completed, cacheEntry{name: e.Name(), modTime: info.ModTime()})
	}

	if len(completed) <= cacheKeepVersions-1 {
		return nil
	}

	sort.Slice(completed, func(i, j int) bool {
		return completed[i].modTime.After(completed[j].modTime)
	})

	for _, victim := range completed[cacheKeepVersions-1:] {
		path := filepath.Join(m.cacheDir, victim.name)
		if rmErr := os.RemoveAll(path); rmErr != nil {
			m.logger.WarnContext(ctx, "failed to remove old runner cache", logging.KeyPath, path, logging.KeyError, rmErr)
			continue
		}
		m.logger.InfoContext(ctx, "removed old runner cache", logging.KeyVersion, victim.name)
	}
	return nil
}

func (m *BinaryManager) lockFor(version string) *sync.Mutex {
	v, _ := m.locks.LoadOrStore(version, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (m *BinaryManager) resolveLatestVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/actions/runner/releases/latest", http.NoBody)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github releases API returned %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("decode release response: %w", err)
	}

	if release.TagName == "" {
		return "", fmt.Errorf("empty tag_name in release response")
	}

	return strings.TrimPrefix(release.TagName, "v"), nil
}

func runnerArch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "x64"
}

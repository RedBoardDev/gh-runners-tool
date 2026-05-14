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
	"strings"
)

type BinaryManager struct {
	cacheDir   string
	logger     *slog.Logger
	httpClient *http.Client
}

func NewBinaryManager(cacheDir string, logger *slog.Logger) *BinaryManager {
	return &BinaryManager{
		cacheDir:   cacheDir,
		logger:     logger,
		httpClient: &http.Client{},
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
		m.logger.InfoContext(ctx, "resolved latest runner version", "version", resolved)
	}

	destDir := filepath.Join(m.cacheDir, resolved)
	runShPath := filepath.Join(destDir, "run.sh")

	if _, err := os.Stat(runShPath); err == nil {
		m.logger.DebugContext(ctx, "runner binary cached", "version", resolved, "path", destDir)
		return destDir, nil
	}

	m.logger.InfoContext(ctx, "downloading runner binary", "version", resolved)

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("create cache dir %s: %w", destDir, err)
	}

	if err := downloadAndExtract(ctx, m.httpClient, resolved, destDir); err != nil {
		rmErr := os.RemoveAll(destDir)
		if rmErr != nil {
			m.logger.WarnContext(ctx, "failed to clean partial download", "path", destDir, "error", rmErr)
		}
		return "", fmt.Errorf("download runner %s: %w", resolved, err)
	}

	m.logger.InfoContext(ctx, "runner binary ready", "version", resolved, "path", destDir)
	return destDir, nil
}

func (m *BinaryManager) resolveLatestVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/actions/runner/releases/latest", nil)
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

package runner

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var sha256DigestRe = regexp.MustCompile(`[0-9a-fA-F]{64}`)

const downloadURLTemplate = "https://github.com/actions/runner/releases/download/v%s/actions-runner-osx-%s-%s.tar.gz"
const releaseAPIURLTemplate = "https://api.github.com/repos/actions/runner/releases/tags/v%s"

func downloadAndExtract(ctx context.Context, client *http.Client, logger *slog.Logger, version, destDir string) error {
	assetName := fmt.Sprintf("actions-runner-osx-%s-%s.tar.gz", runnerArch(), version)
	url := fmt.Sprintf(downloadURLTemplate, version, runnerArch(), version)

	checksumURL := fmt.Sprintf(releaseAPIURLTemplate, version)
	logger.DebugContext(ctx, "fetching runner checksum", "url", checksumURL, "asset", assetName)
	expected, err := fetchRunnerReleaseChecksum(ctx, client, checksumURL, assetName)
	if err != nil {
		return fmt.Errorf("fetch checksum for %s: %w", url, err)
	}
	logger.DebugContext(ctx, "checksum fetched", "sha256", expected[:8]+"...")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}

	logger.DebugContext(ctx, "starting tarball download", "url", url)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download tarball: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d for %s", resp.StatusCode, url)
	}

	logger.DebugContext(ctx, "downloading to temp file", "content_length", resp.ContentLength)
	tmp, err := os.CreateTemp("", "ghr-runner-*.tar.gz")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	hasher := sha256.New()
	if _, err := io.Copy(tmp, io.TeeReader(resp.Body, hasher)); err != nil {
		return fmt.Errorf("download tarball: %w", err)
	}
	resp.Body.Close()

	if expected != "" {
		got := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(got, expected) {
			return fmt.Errorf("checksum mismatch for %s: got %s, want %s", url, got, expected)
		}
	}

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek temp file: %w", err)
	}

	logger.DebugContext(ctx, "extracting runner archive")
	return extractTarGz(tmp, destDir)
}

func fetchExpectedSHA256(ctx context.Context, client *http.Client, url string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("create checksum request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch checksum: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("checksum URL returned HTTP %d for %s", resp.StatusCode, url)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksum URL returned HTTP %d for %s", resp.StatusCode, url)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		return "", fmt.Errorf("read checksum response: %w", err)
	}

	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return "", fmt.Errorf("checksum file %s is empty", url)
	}
	hash := strings.ToLower(fields[0])
	if !isSHA256Digest(hash) {
		return "", fmt.Errorf("checksum %q from %s is not a sha-256 digest", hash, url)
	}
	return hash, nil
}

func fetchRunnerReleaseChecksum(ctx context.Context, client *http.Client, url, assetName string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("create release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("release URL returned HTTP %d for %s", resp.StatusCode, url)
	}

	var release struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		return "", fmt.Errorf("decode release response: %w", err)
	}

	hash, err := checksumFromReleaseBody(release.Body, assetName)
	if err != nil {
		return "", fmt.Errorf("release checksum for %s: %w", assetName, err)
	}
	return hash, nil
}

func checksumFromReleaseBody(body, assetName string) (string, error) {
	for _, line := range strings.Split(body, "\n") {
		if !strings.Contains(line, assetName) {
			continue
		}
		if m := sha256DigestRe.FindString(line); m != "" {
			return strings.ToLower(m), nil
		}
	}
	return "", fmt.Errorf("no sha-256 digest found for asset %s", assetName)
}

func isSHA256Digest(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func extractTarGz(r io.Reader, destDir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("open gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		target, err := sanitizeTarPath(destDir, header.Name)
		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("create directory %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := extractFile(tr, target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := writeSymlink(destDir, target, header.Name, header.Linkname); err != nil {
				return err
			}
		}
	}

	return nil
}

func extractFile(r io.Reader, path string, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create parent dir for %s: %w", path, err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("create file %s: %w", path, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("write file %s: %w", path, err)
	}

	return nil
}

func sanitizeTarPath(destDir, name string) (string, error) {
	target := filepath.Join(destDir, filepath.Clean(name))
	if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) && target != filepath.Clean(destDir) {
		return "", fmt.Errorf("tar entry %q escapes destination directory", name)
	}
	return target, nil
}

func writeSymlink(destDir, target, name, linkname string) error {
	if filepath.IsAbs(linkname) {
		return fmt.Errorf("tar symlink %q -> %q has absolute target", name, linkname)
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(target), linkname))
	cleanDest := filepath.Clean(destDir)
	if resolved != cleanDest && !strings.HasPrefix(resolved, cleanDest+string(os.PathSeparator)) {
		return fmt.Errorf("tar symlink %q -> %q escapes destination directory", name, linkname)
	}
	if err := os.Symlink(linkname, target); err != nil {
		return fmt.Errorf("create symlink %s: %w", target, err)
	}
	return nil
}

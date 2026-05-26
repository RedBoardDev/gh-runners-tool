package runner

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const downloadURLTemplate = "https://github.com/actions/runner/releases/download/v%s/actions-runner-osx-%s-%s.tar.gz"

func downloadAndExtract(ctx context.Context, client *http.Client, logger *slog.Logger, version, destDir string) error {
	url := fmt.Sprintf(downloadURLTemplate, version, runnerArch(), version)

	logger.DebugContext(ctx, "fetching runner checksum", "url", url+".sha256")
	expected, err := fetchExpectedSHA256(ctx, client, url+".sha256")
	if err != nil {
		return fmt.Errorf("fetch checksum for %s: %w", url, err)
	}
	if expected != "" {
		logger.DebugContext(ctx, "checksum fetched", "sha256", expected[:8]+"...")
	} else {
		logger.WarnContext(ctx, "no checksum file available, skipping verification")
	}

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
	if err := extractTarGz(tmp, destDir); err != nil {
		return err
	}
	return nil
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
		return "", nil
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
	if len(hash) != 64 {
		return "", fmt.Errorf("checksum %q from %s is not a sha-256 digest", hash, url)
	}
	return hash, nil
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

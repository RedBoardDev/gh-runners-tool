package runner

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const downloadURLTemplate = "https://github.com/actions/runner/releases/download/v%s/actions-runner-osx-%s-%s.tar.gz"

func downloadAndExtract(ctx context.Context, client *http.Client, version, destDir string) error {
	url := fmt.Sprintf(downloadURLTemplate, version, runnerArch(), version)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download tarball: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d for %s", resp.StatusCode, url)
	}

	return extractTarGz(resp.Body, destDir)
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
			if err := writeSymlink(target, header.Name, header.Linkname); err != nil {
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

func writeSymlink(target, name, linkname string) error {
	if filepath.IsAbs(linkname) || !filepath.IsLocal(linkname) {
		return fmt.Errorf("tar symlink %q -> %q is not local", name, linkname)
	}
	if err := os.Symlink(linkname, target); err != nil {
		return fmt.Errorf("create symlink %s: %w", target, err)
	}
	return nil
}

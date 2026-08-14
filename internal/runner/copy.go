package runner

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("walk source %s: %w", path, err)
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("compute relative path for %s: %w", path, err)
		}

		targetPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(targetPath, info.Mode())
		}

		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("read symlink %s: %w", path, err)
			}
			if filepath.IsAbs(link) {
				return fmt.Errorf("refusing to copy symlink %s -> %q (absolute target)", path, link)
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(path), link))
			cleanSrc := filepath.Clean(src)
			if resolved != cleanSrc && !strings.HasPrefix(resolved, cleanSrc+string(os.PathSeparator)) {
				return fmt.Errorf("refusing to copy symlink %s -> %q (escapes source directory)", path, link)
			}
			return os.Symlink(link, targetPath)
		}

		return copyFile(path, targetPath, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	// Link instead of duplicating the bytes: the runner bits are read-only for
	// the job's lifetime, and a full copy rewrites the whole runner release per
	// provisioned runner. Make anything write to a linked file IN PLACE and the
	// change lands in the shared cache, breaking every runner provisioned after
	// it. Any failure here (cross-device, link limit) falls through to the copy.
	if err := os.Link(src, dst); err == nil {
		return nil
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("create dest %s: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}

	return nil
}

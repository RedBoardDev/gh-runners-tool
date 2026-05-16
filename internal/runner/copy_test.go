package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyDir_RefusesAbsoluteSymlink(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	if err := os.Symlink("/etc/passwd", filepath.Join(src, "evil-link")); err != nil {
		t.Fatalf("setup symlink: %v", err)
	}

	err := copyDir(src, dst)
	if err == nil {
		t.Fatal("expected error for absolute symlink, got nil")
	}
	if !strings.Contains(err.Error(), "non-local") {
		t.Errorf("error = %v, want substring 'non-local'", err)
	}
}

func TestCopyDir_RefusesEscapeSymlink(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	if err := os.Symlink("../../etc/passwd", filepath.Join(src, "escape-link")); err != nil {
		t.Fatalf("setup symlink: %v", err)
	}

	err := copyDir(src, dst)
	if err == nil {
		t.Fatal("expected error for escaping symlink, got nil")
	}
	if !strings.Contains(err.Error(), "non-local") {
		t.Errorf("error = %v, want substring 'non-local'", err)
	}
}

func TestCopyDir_AllowsLocalSymlink(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	if err := os.WriteFile(filepath.Join(src, "target.sh"), []byte("ok"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink("target.sh", filepath.Join(src, "link.sh")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir: %v", err)
	}

	link, err := os.Readlink(filepath.Join(dst, "link.sh"))
	if err != nil {
		t.Fatalf("readlink dst: %v", err)
	}
	if link != "target.sh" {
		t.Errorf("link = %q, want %q", link, "target.sh")
	}
}

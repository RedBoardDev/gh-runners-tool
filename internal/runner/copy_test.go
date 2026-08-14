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
	if !strings.Contains(err.Error(), "absolute target") {
		t.Errorf("error = %v, want substring 'absolute target'", err)
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
	if !strings.Contains(err.Error(), "escapes source directory") {
		t.Errorf("error = %v, want substring 'escapes source directory'", err)
	}
}

func TestCopyDir_LinksRegularFilesInsteadOfDuplicating(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	if err := os.WriteFile(filepath.Join(src, "run.sh"), []byte("ok"), 0o755); err != nil {
		t.Fatalf("write source: %v", err)
	}

	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir: %v", err)
	}

	srcInfo, err := os.Stat(filepath.Join(src, "run.sh"))
	if err != nil {
		t.Fatalf("stat source: %v", err)
	}
	dstInfo, err := os.Stat(filepath.Join(dst, "run.sh"))
	if err != nil {
		t.Fatalf("stat dest: %v", err)
	}

	if !os.SameFile(srcInfo, dstInfo) {
		t.Error("runner bits were duplicated; every provisioned runner rewrites the whole release")
	}
}

func TestCopyFile_CopiesWhenLinkIsRefused(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.sh")
	dst := filepath.Join(dir, "dst.sh")

	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	// An existing destination makes os.Link fail, standing in for the
	// cross-device case the fallback really exists for.
	if err := os.WriteFile(dst, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write dest: %v", err)
	}

	if err := copyFile(src, dst, 0o644); err != nil {
		t.Fatalf("copyFile: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != "payload" {
		t.Errorf("dest = %q, want %q", got, "payload")
	}

	srcInfo, err := os.Stat(src)
	if err != nil {
		t.Fatalf("stat source: %v", err)
	}
	dstInfo, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat dest: %v", err)
	}
	if os.SameFile(srcInfo, dstInfo) {
		t.Error("fallback linked the files; a failed link must leave an independent copy")
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

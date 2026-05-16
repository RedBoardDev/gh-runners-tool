package runner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type tarEntry struct {
	name     string
	typeflag byte
	body     string
	linkname string
	mode     int64
}

func buildTarGz(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.name,
			Mode:     e.mode,
			Typeflag: e.typeflag,
			Size:     int64(len(e.body)),
			Linkname: e.linkname,
		}
		if e.typeflag == 0 {
			hdr.Typeflag = tar.TypeReg
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header: %v", err)
		}
		if e.typeflag == tar.TypeReg || e.typeflag == 0 {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatalf("write body: %v", err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gz: %v", err)
	}
	return buf.Bytes()
}

func TestExtractTarGz_BlocksAbsoluteSymlink(t *testing.T) {
	dest := t.TempDir()
	data := buildTarGz(t, []tarEntry{
		{name: "evil-link", typeflag: tar.TypeSymlink, linkname: "/etc/passwd"},
	})

	err := extractTarGz(bytes.NewReader(data), dest)
	if err == nil {
		t.Fatal("expected error for absolute symlink, got nil")
	}
	if !strings.Contains(err.Error(), "not local") {
		t.Errorf("error = %v, want substring 'not local'", err)
	}
	if _, statErr := os.Lstat(filepath.Join(dest, "evil-link")); statErr == nil {
		t.Error("symlink should not have been created")
	}
}

func TestExtractTarGz_BlocksRelativeEscapeSymlink(t *testing.T) {
	dest := t.TempDir()
	data := buildTarGz(t, []tarEntry{
		{name: "escape-link", typeflag: tar.TypeSymlink, linkname: "../../etc/passwd"},
	})

	err := extractTarGz(bytes.NewReader(data), dest)
	if err == nil {
		t.Fatal("expected error for escaping symlink, got nil")
	}
	if !strings.Contains(err.Error(), "not local") {
		t.Errorf("error = %v, want substring 'not local'", err)
	}
}

func TestExtractTarGz_BlocksPathTraversal(t *testing.T) {
	dest := t.TempDir()
	data := buildTarGz(t, []tarEntry{
		{name: "../escape.sh", typeflag: tar.TypeReg, body: "evil", mode: 0o644},
	})

	err := extractTarGz(bytes.NewReader(data), dest)
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
	if !strings.Contains(err.Error(), "escapes destination") {
		t.Errorf("error = %v, want substring 'escapes destination'", err)
	}
}

func TestExtractTarGz_AllowsLocalSymlink(t *testing.T) {
	dest := t.TempDir()
	data := buildTarGz(t, []tarEntry{
		{name: "target.sh", typeflag: tar.TypeReg, body: "ok", mode: 0o755},
		{name: "link.sh", typeflag: tar.TypeSymlink, linkname: "target.sh"},
	})

	if err := extractTarGz(bytes.NewReader(data), dest); err != nil {
		t.Fatalf("extractTarGz: %v", err)
	}
	link, err := os.Readlink(filepath.Join(dest, "link.sh"))
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if link != "target.sh" {
		t.Errorf("link = %q, want %q", link, "target.sh")
	}
}

package runner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
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

func TestFetchExpectedSHA256(t *testing.T) {
	const valid = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	cases := []struct {
		name       string
		statusCode int
		body       string
		wantHash   string
		wantErr    string
	}{
		{
			name:       "hash with filename",
			statusCode: http.StatusOK,
			body:       valid + "  actions-runner-osx-arm64-2.331.0.tar.gz\n",
			wantHash:   valid,
		},
		{
			name:       "hash only",
			statusCode: http.StatusOK,
			body:       valid + "\n",
			wantHash:   valid,
		},
		{
			name:       "empty body",
			statusCode: http.StatusOK,
			body:       "",
			wantErr:    "empty",
		},
		{
			name:       "non-sha digest",
			statusCode: http.StatusOK,
			body:       "shortdigest\n",
			wantErr:    "not a sha-256",
		},
		{
			name:       "404",
			statusCode: http.StatusNotFound,
			body:       "",
			wantErr:    "HTTP 404",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			got, err := fetchExpectedSHA256(context.Background(), srv.Client(), srv.URL)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantHash {
				t.Errorf("hash = %q, want %q", got, tc.wantHash)
			}
		})
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

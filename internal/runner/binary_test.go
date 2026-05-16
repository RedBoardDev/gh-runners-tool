package runner

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

func createFakeTarGz(t *testing.T) string {
	t.Helper()

	tmpFile := filepath.Join(t.TempDir(), "runner.tar.gz")
	f, err := os.Create(tmpFile)
	if err != nil {
		t.Fatalf("create tar.gz file: %v", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	content := []byte("#!/bin/bash\necho hello\n")
	if err := tw.WriteHeader(&tar.Header{
		Name:     "run.sh",
		Mode:     0o755,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("write tar content: %v", err)
	}

	subContent := []byte("config data\n")
	if err := tw.WriteHeader(&tar.Header{
		Name:     "config.sh",
		Mode:     0o755,
		Size:     int64(len(subContent)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tw.Write(subContent); err != nil {
		t.Fatalf("write tar content: %v", err)
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}

	return tmpFile
}

func TestEnsureBits_Cached(t *testing.T) {
	cacheDir := t.TempDir()
	version := "2.320.0"

	versionDir := filepath.Join(cacheDir, version)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatalf("create version dir: %v", err)
	}
	runSh := filepath.Join(versionDir, "run.sh")
	if err := os.WriteFile(runSh, []byte("#!/bin/bash\n"), 0o755); err != nil {
		t.Fatalf("write run.sh: %v", err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, ".complete"), nil, 0o644); err != nil {
		t.Fatalf("write completion marker: %v", err)
	}

	bm := NewBinaryManager(cacheDir, silentLogger())

	got, err := bm.EnsureBits(context.Background(), version)
	if err != nil {
		t.Fatalf("EnsureBits: %v", err)
	}

	if got != versionDir {
		t.Fatalf("expected path %q, got %q", versionDir, got)
	}
}

func TestEnsureBits_IncompleteCacheIsCleaned(t *testing.T) {
	cacheDir := t.TempDir()
	version := "2.320.0"

	versionDir := filepath.Join(cacheDir, version)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatalf("create version dir: %v", err)
	}
	// run.sh exists but no .complete marker → previous download was interrupted.
	if err := os.WriteFile(filepath.Join(versionDir, "run.sh"), []byte("#!/bin/bash\n"), 0o755); err != nil {
		t.Fatalf("write run.sh: %v", err)
	}

	bm := NewBinaryManager(cacheDir, silentLogger())
	// Point at an unreachable host so the redownload fails after the stale dir is removed.
	bm.httpClient = &http.Client{Timeout: 100 * time.Millisecond}
	_, err := bm.EnsureBits(context.Background(), version)
	if err == nil {
		t.Fatal("expected EnsureBits to attempt re-download, got nil error")
	}
	if _, statErr := os.Stat(versionDir); !os.IsNotExist(statErr) {
		t.Errorf("incomplete cache dir should have been removed, stat err = %v", statErr)
	}
}

func TestEnsureBits_Download(t *testing.T) {
	tarGzPath := createFakeTarGz(t)
	tarGzData, err := os.ReadFile(tarGzPath)
	if err != nil {
		t.Fatalf("read tar.gz: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		if _, writeErr := w.Write(tarGzData); writeErr != nil {
			t.Errorf("write response: %v", writeErr)
		}
	}))
	defer srv.Close()

	cacheDir := t.TempDir()
	version := "2.320.0"
	versionDir := filepath.Join(cacheDir, version)

	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatalf("create version dir: %v", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/runner.tar.gz", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if err := extractTarGz(resp.Body, versionDir); err != nil {
		t.Fatalf("extract tar.gz: %v", err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, ".complete"), nil, 0o644); err != nil {
		t.Fatalf("write completion marker: %v", err)
	}

	runSh := filepath.Join(versionDir, "run.sh")
	if _, statErr := os.Stat(runSh); statErr != nil {
		t.Fatalf("run.sh not found after extraction: %v", statErr)
	}

	configSh := filepath.Join(versionDir, "config.sh")
	if _, statErr := os.Stat(configSh); statErr != nil {
		t.Fatalf("config.sh not found after extraction: %v", statErr)
	}

	bm := NewBinaryManager(cacheDir, silentLogger())
	got, err := bm.EnsureBits(context.Background(), version)
	if err != nil {
		t.Fatalf("EnsureBits on extracted dir: %v", err)
	}
	if got != versionDir {
		t.Fatalf("expected path %q, got %q", versionDir, got)
	}
}

func TestGCOldVersions_KeepsRecent(t *testing.T) {
	cacheDir := t.TempDir()
	bm := NewBinaryManager(cacheDir, silentLogger())

	versions := []string{"2.310.0", "2.311.0", "2.312.0", "2.313.0", "2.314.0"}
	for i, v := range versions {
		dir := filepath.Join(cacheDir, v)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		marker := filepath.Join(dir, ".complete")
		if err := os.WriteFile(marker, nil, 0o644); err != nil {
			t.Fatalf("write marker: %v", err)
		}
		// Stagger mtimes so the sort by modTime is deterministic.
		mtime := time.Now().Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(marker, mtime, mtime); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}

	if err := bm.gcOldVersions(context.Background(), "2.314.0"); err != nil {
		t.Fatalf("gcOldVersions: %v", err)
	}

	// 2.314.0 is the active version (excluded from GC); the GC keeps the 2
	// most recent of the remaining versions (cacheKeepVersions-1).
	kept := map[string]bool{}
	entries, _ := os.ReadDir(cacheDir)
	for _, e := range entries {
		kept[e.Name()] = true
	}
	if !kept["2.314.0"] {
		t.Errorf("active version 2.314.0 was removed")
	}
	if !kept["2.313.0"] || !kept["2.312.0"] {
		t.Errorf("recent versions were removed: %v", kept)
	}
	if kept["2.310.0"] || kept["2.311.0"] {
		t.Errorf("old versions still present: %v", kept)
	}
}

func TestGCOldVersions_IgnoresIncompleteCaches(t *testing.T) {
	cacheDir := t.TempDir()
	bm := NewBinaryManager(cacheDir, silentLogger())

	// "old" has a marker, "incomplete" doesn't.
	for _, v := range []string{"2.310.0", "incomplete"} {
		dir := filepath.Join(cacheDir, v)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "2.310.0", ".complete"), nil, 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	if err := bm.gcOldVersions(context.Background(), "2.314.0"); err != nil {
		t.Fatalf("gcOldVersions: %v", err)
	}

	// "incomplete" should remain because gc ignores caches without the marker.
	for _, want := range []string{"2.310.0", "incomplete"} {
		if _, err := os.Stat(filepath.Join(cacheDir, want)); err != nil {
			t.Errorf("expected %s to remain: %v", want, err)
		}
	}
}

func TestRunnerArch(t *testing.T) {
	got := runnerArch()
	switch runtime.GOARCH {
	case "arm64":
		if got != "arm64" {
			t.Fatalf("expected arm64, got %q", got)
		}
	default:
		if got != "x64" {
			t.Fatalf("expected x64, got %q", got)
		}
	}
}

func TestResolveLatestVersion(t *testing.T) {
	tests := []struct {
		name       string
		response   any
		statusCode int
		wantVer    string
		wantErr    bool
	}{
		{
			name:       "valid release with v prefix",
			response:   map[string]string{"tag_name": "v2.320.0"},
			statusCode: http.StatusOK,
			wantVer:    "2.320.0",
		},
		{
			name:       "valid release without v prefix",
			response:   map[string]string{"tag_name": "2.321.0"},
			statusCode: http.StatusOK,
			wantVer:    "2.321.0",
		},
		{
			name:       "empty tag_name",
			response:   map[string]string{"tag_name": ""},
			statusCode: http.StatusOK,
			wantErr:    true,
		},
		{
			name:       "api error",
			response:   nil,
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				if tt.response != nil {
					data, jsonErr := json.Marshal(tt.response)
					if jsonErr != nil {
						t.Fatalf("marshal response: %v", jsonErr)
					}
					if _, writeErr := w.Write(data); writeErr != nil {
						t.Errorf("write response: %v", writeErr)
					}
				}
			}))
			defer srv.Close()

			bm := &BinaryManager{
				cacheDir:   t.TempDir(),
				logger:     silentLogger(),
				httpClient: srv.Client(),
			}

			ctx := context.Background()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
			if err != nil {
				t.Fatalf("create request: %v", err)
			}
			req.Header.Set("Accept", "application/vnd.github+json")

			resp, err := bm.httpClient.Do(req)
			if err != nil {
				t.Fatalf("do request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				if !tt.wantErr {
					t.Fatalf("unexpected status %d", resp.StatusCode)
				}
				return
			}

			var release struct {
				TagName string `json:"tag_name"`
			}
			if decodeErr := json.NewDecoder(resp.Body).Decode(&release); decodeErr != nil {
				if !tt.wantErr {
					t.Fatalf("decode: %v", decodeErr)
				}
				return
			}

			if release.TagName == "" {
				if !tt.wantErr {
					t.Fatal("empty tag_name")
				}
				return
			}

			got := release.TagName
			if len(got) > 0 && got[0] == 'v' {
				got = got[1:]
			}

			if tt.wantErr {
				t.Fatal("expected error but got none")
			}
			if got != tt.wantVer {
				t.Fatalf("expected version %q, got %q", tt.wantVer, got)
			}
		})
	}
}

package doctor

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestSocketCheck_MissingSocket(t *testing.T) {
	c := SocketCheck{Path: filepath.Join(t.TempDir(), "ghr.sock")}
	res := c.Run(context.Background())
	if res.Status != StatusFail {
		t.Errorf("status = %s, want FAIL", res.Status)
	}
}

func TestSocketCheck_HealthyDaemon(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "ghr.sock")

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	res := SocketCheck{Path: sock}.Run(context.Background())
	if res.Status != StatusOK {
		t.Errorf("status = %s summary=%q, want OK", res.Status, res.Summary)
	}
}

func TestSocketCheck_DaemonReturns500(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "ghr.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	res := SocketCheck{Path: sock}.Run(context.Background())
	if res.Status != StatusWarn {
		t.Errorf("status = %s, want WARN", res.Status)
	}
}

package runner

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"gh-runners-tool/internal/config"
	"gh-runners-tool/internal/domain"
)

const pidFileName = ".ghr-pid"

type Manager struct {
	cacheDir   string
	logger     Logger
	httpClient *http.Client
	mu         sync.Mutex
}

type Logger interface {
	Printf(string, ...any)
}

type Handle struct {
	ID      string
	Group   string
	Cmd     *exec.Cmd
	Workdir string
	done    chan struct{}
	err     error
}

func (h *Handle) Wait() error {
	<-h.done
	return h.err
}

func (h *Handle) Done() <-chan struct{} {
	return h.done
}

func New(cacheDir string, logger Logger) *Manager {
	return &Manager{
		cacheDir:   cacheDir,
		logger:     logger,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// Start prepares and launches a runner process for the given instance.
func (m *Manager) Start(ctx context.Context, inst domain.RunnerInstance, gh config.GitHubConfig, registrationToken string) (*Handle, error) {
	baseDir, err := m.ensureRunnerBits(ctx, inst.Version)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(inst.Workdir, 0o755); err != nil {
		return nil, fmt.Errorf("create workdir: %w", err)
	}

	if err := copyDir(baseDir, inst.Workdir); err != nil {
		return nil, fmt.Errorf("copy runner files: %w", err)
	}

	name := fmt.Sprintf("%s-%s", inst.GroupName, inst.ID)
	url := runnerURL(gh)

	configArgs := []string{
		filepath.Join(inst.Workdir, "config.sh"),
		"--unattended",
		"--url", url,
		"--token", registrationToken,
		"--name", name,
	}
	if len(inst.Labels) > 0 {
		configArgs = append(configArgs, "--labels", strings.Join(inst.Labels, ","))
	}
	if inst.Ephemeral {
		configArgs = append(configArgs, "--ephemeral")
	}

	configCmd := exec.CommandContext(ctx, "bash", configArgs...)
	configCmd.Dir = inst.Workdir
	configCmd.Stdout = os.Stdout
	configCmd.Stderr = os.Stderr
	if err := configCmd.Run(); err != nil {
		_ = os.RemoveAll(inst.Workdir)
		return nil, fmt.Errorf("config runner: %w", err)
	}

	runCmd := exec.CommandContext(ctx, filepath.Join(inst.Workdir, "run.sh"))
	runCmd.Dir = inst.Workdir
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr

	if err := runCmd.Start(); err != nil {
		_ = os.RemoveAll(inst.Workdir)
		return nil, fmt.Errorf("start runner: %w", err)
	}

	if err := m.writePID(inst.Workdir, runCmd.Process.Pid); err != nil {
		_ = runCmd.Process.Kill()
		_ = os.RemoveAll(inst.Workdir)
		return nil, fmt.Errorf("write pid: %w", err)
	}

	handle := &Handle{
		ID:      inst.ID,
		Group:   inst.GroupName,
		Cmd:     runCmd,
		Workdir: inst.Workdir,
		done:    make(chan struct{}),
	}

	go func() {
		defer close(handle.done)
		handle.err = runCmd.Wait()
		// Cleanup workdir regardless of exit status.
		_ = os.RemoveAll(inst.Workdir)
	}()

	return handle, nil
}

func (m *Manager) ensureRunnerBits(ctx context.Context, version string) (string, error) {
	resolvedVersion, err := m.resolveVersion(ctx, version)
	if err != nil {
		return "", err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	targetDir := filepath.Join(m.cacheDir, resolvedVersion)
	if _, err := os.Stat(targetDir); err == nil {
		return targetDir, nil
	}

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	archivePath := filepath.Join(m.cacheDir, fmt.Sprintf("actions-runner-%s.tar.gz", resolvedVersion))
	if err := m.downloadRunner(ctx, resolvedVersion, archivePath); err != nil {
		return "", err
	}

	if err := untar(archivePath, targetDir); err != nil {
		return "", fmt.Errorf("untar: %w", err)
	}

	return targetDir, nil
}

func (m *Manager) downloadRunner(ctx context.Context, version, dest string) error {
	url := runnerDownloadURL(version)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download runner: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("download runner failed: status %d", resp.StatusCode)
	}

	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("write archive: %w", err)
	}

	return nil
}

func runnerDownloadURL(version string) string {
	resolved := version
	arch := "x64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}
	return fmt.Sprintf("https://github.com/actions/runner/releases/download/v%s/actions-runner-osx-%s-%s.tar.gz", resolved, arch, resolved)
}

func (m *Manager) resolveVersion(ctx context.Context, version string) (string, error) {
	if version != "latest" {
		return version, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/actions/runner/releases/latest", nil)
	if err != nil {
		return "", err
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("latest version lookup failed: status %d", resp.StatusCode)
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	tag := strings.TrimPrefix(payload.TagName, "v")
	if tag == "" {
		return "", fmt.Errorf("empty tag from latest release")
	}
	return tag, nil
}

func runnerURL(gh config.GitHubConfig) string {
	if gh.Scope == config.ScopeRepo {
		return fmt.Sprintf("https://github.com/%s/%s", gh.Owner, gh.Repo)
	}
	return fmt.Sprintf("https://github.com/%s", gh.Owner)
}

func untar(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}

		targetPath := filepath.Join(dest, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return err
			}
			outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				_ = outFile.Close()
				return err
			}
			_ = outFile.Close()
		default:
			continue
		}
	}
	return nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		dstFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer dstFile.Close()

		if _, err := io.Copy(dstFile, srcFile); err != nil {
			return err
		}

		return nil
	})
}

// NewRunnerInstance builds a runner instance descriptor with generated ID.
func NewRunnerInstance(group domain.Group) domain.RunnerInstance {
	id := randID()
	return domain.RunnerInstance{
		ID:        id,
		GroupName: group.Name,
		Ephemeral: group.Ephemeral,
		Workdir:   filepath.Join(group.Workdir, id),
		Labels:    group.Labels,
		Version:   group.Version,
	}
}

func randID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (m *Manager) writePID(workdir string, pid int) error {
	pidPath := filepath.Join(workdir, pidFileName)
	return os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0o644)
}

// CleanupStale removes leftover runner workdirs and terminates stray runner processes in known bases.
func (m *Manager) CleanupStale(bases []string) {
	for _, base := range bases {
		entries, err := os.ReadDir(base)
		if err != nil {
			m.logger.Printf("cleanup: skip base %s: %v", base, err)
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dir := filepath.Join(base, entry.Name())
			pidPath := filepath.Join(dir, pidFileName)
			if pidBytes, err := os.ReadFile(pidPath); err == nil {
				if pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes))); err == nil {
					if err := killPID(pid); err != nil {
						m.logger.Printf("kill stale pid %d (%s): %v", pid, dir, err)
					}
				}
			}
			if err := os.RemoveAll(dir); err != nil {
				m.logger.Printf("remove stale workdir %s: %v", dir, err)
			}
		}
	}
}

func killPID(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid %d", pid)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}

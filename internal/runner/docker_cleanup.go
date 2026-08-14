package runner

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/logging"
)

const (
	dockerCleanupTimeout = 30 * time.Second
	dockerRemoveTimeout  = 5 * time.Minute
	jobNetworkPrefix     = "github_network_"
	gcImage              = "alpine:3.22"
)

// CleanupJobContainers removes docker containers (and their job networks)
// created by the runner whose workdir is given. The runner's job container
// bind-mounts paths under the workdir; its service containers (e.g. postgres,
// redis) don't, so they are found through the shared github_network_* network.
// Best-effort: hosts without docker are a no-op, and any docker failure only
// logs — workdir removal must never be blocked by container cleanup.
func (m *ProcessManager) CleanupJobContainers(ctx context.Context, workdir string) {
	if _, err := exec.LookPath("docker"); err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dockerCleanupTimeout)
	defer cancel()

	containers, err := listContainers(ctx)
	if err != nil {
		m.logger.DebugContext(ctx, "docker cleanup skipped", logging.KeyDir, workdir, logging.KeyError, err)
		return
	}

	ids, networks := selectJobContainers(containers, workdir)
	if len(ids) == 0 {
		return
	}

	if out, rmErr := exec.CommandContext(ctx, "docker", append([]string{"rm", "-f"}, ids...)...).CombinedOutput(); rmErr != nil {
		m.logger.WarnContext(ctx, "failed to remove job containers",
			logging.KeyDir, workdir, logging.KeyError, rmErr, "output", strings.TrimSpace(string(out)))
	} else {
		m.logger.InfoContext(ctx, "removed job containers", logging.KeyDir, workdir, "count", len(ids))
	}

	for _, net := range networks {
		if out, netErr := exec.CommandContext(ctx, "docker", "network", "rm", net).CombinedOutput(); netErr != nil {
			m.logger.DebugContext(ctx, "failed to remove job network",
				"network", net, logging.KeyError, netErr, "output", strings.TrimSpace(string(out)))
		}
	}
}

// RemoveDirWithDockerFallback removes dir like os.RemoveAll, but retries
// through a root container when files written by job containers (owned by
// uid 0 on bind mounts) make the plain removal fail with EPERM/EACCES.
func (m *ProcessManager) RemoveDirWithDockerFallback(ctx context.Context, dir string) error {
	err := os.RemoveAll(dir)
	if err == nil {
		return nil
	}
	if !errors.Is(err, fs.ErrPermission) {
		return err
	}
	if _, lookErr := exec.LookPath("docker"); lookErr != nil {
		return err
	}

	parent := filepath.Dir(filepath.Clean(dir))
	base := filepath.Base(filepath.Clean(dir))
	if parent == "/" || base == "." || base == "/" {
		return err
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dockerRemoveTimeout)
	defer cancel()

	m.logger.InfoContext(ctx, "removing root-owned workdir via docker", logging.KeyDir, dir)
	out, dockerErr := exec.CommandContext(ctx, "docker", "run", "--rm",
		"-v", parent+":/ghr-gc", gcImage, "rm", "-rf", "/ghr-gc/"+base).CombinedOutput()
	if dockerErr != nil {
		m.logger.WarnContext(ctx, "docker fallback removal failed",
			logging.KeyDir, dir, logging.KeyError, dockerErr, "output", strings.TrimSpace(string(out)))
		return err
	}
	return nil
}

type containerInfo struct {
	id       string
	mounts   []string
	networks []string
}

func listContainers(ctx context.Context) ([]containerInfo, error) {
	out, err := exec.CommandContext(ctx, "docker", "ps", "-aq").Output()
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return nil, nil
	}

	const format = `{{.Id}}|{{range .Mounts}}{{.Source}},{{end}}|{{range $k, $v := .NetworkSettings.Networks}}{{$k}},{{end}}`
	inspectOut, err := exec.CommandContext(ctx, "docker", append([]string{"inspect", "--format", format}, ids...)...).Output()
	if err != nil {
		return nil, err
	}
	return parseContainerLines(string(inspectOut)), nil
}

func parseContainerLines(out string) []containerInfo {
	var containers []containerInfo
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 || parts[0] == "" {
			continue
		}
		containers = append(containers, containerInfo{
			id:       parts[0],
			mounts:   splitList(parts[1]),
			networks: splitList(parts[2]),
		})
	}
	return containers
}

func splitList(s string) []string {
	var items []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

// selectJobContainers returns the ids of every container tied to the workdir —
// directly via a bind mount, or indirectly via a github_network_* shared with
// a mount-matched container — plus the job networks to remove.
func selectJobContainers(containers []containerInfo, workdir string) ([]string, []string) {
	prefix := strings.TrimRight(workdir, "/") + "/"
	jobNetworks := map[string]bool{}

	for _, c := range containers {
		for _, mount := range c.mounts {
			if strings.HasPrefix(mount, prefix) || mount == strings.TrimRight(workdir, "/") {
				for _, net := range c.networks {
					if strings.HasPrefix(net, jobNetworkPrefix) {
						jobNetworks[net] = true
					}
				}
				break
			}
		}
	}

	var ids, networks []string
	for _, c := range containers {
		matched := false
		for _, mount := range c.mounts {
			if strings.HasPrefix(mount, prefix) {
				matched = true
				break
			}
		}
		if !matched {
			for _, net := range c.networks {
				if jobNetworks[net] {
					matched = true
					break
				}
			}
		}
		if matched {
			ids = append(ids, c.id)
		}
	}
	for net := range jobNetworks {
		networks = append(networks, net)
	}
	return ids, networks
}

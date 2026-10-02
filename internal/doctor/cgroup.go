package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/cgroup"
)

type CgroupCheck struct {
	Enabled  bool
	NeedCPU  bool
	Root     string
	ProcRoot string
	PIDFile  string
}

func (c CgroupCheck) Name() string { return "cgroup" }

func (c CgroupCheck) Run(_ context.Context) Result {
	res := Result{Name: c.Name()}
	if !c.Enabled {
		res.Status = StatusSkip
		res.Summary = "no group sets resources"
		return res
	}

	mgr := cgroup.NewManager(c.Root, nil)
	if c.ProcRoot != "" {
		mgr.UseProcRoot(c.ProcRoot)
	}
	pid, running := daemonPID(c.PIDFile)
	if !running {
		return c.notRunningResult(&res, mgr)
	}

	base, err := mgr.VerifyEnforcement(pid, c.NeedCPU)
	if err != nil {
		res.Status = StatusFail
		res.Summary = "per-runner cgroup limits are not enforced"
		res.Details = []string{err.Error()}
		res.Hint = "run ghr under a systemd service with Delegate=yes and OOMPolicy=continue, then restart it"
		return res
	}

	res.Status = StatusOK
	res.Summary = "per-runner cgroup limits enforced"
	res.Details = []string{fmt.Sprintf("delegated cgroup: %s", base)}
	return res
}

func (c CgroupCheck) notRunningResult(res *Result, mgr *cgroup.Manager) Result {
	if err := mgr.CheckUnified(); err != nil {
		res.Status = StatusFail
		res.Summary = "cgroup v2 is not available"
		res.Details = []string{err.Error()}
		res.Hint = "per-runner limits need a Linux host booted with the unified cgroup v2 hierarchy"
		return *res
	}
	res.Status = StatusWarn
	res.Summary = "daemon not running: delegation can only be verified against the running daemon"
	res.Hint = "start ghr, then rerun; the systemd unit needs Delegate=yes and OOMPolicy=continue"
	return *res
}

func daemonPID(pidFile string) (int, bool) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	if err := syscall.Kill(pid, 0); err != nil && !errors.Is(err, syscall.EPERM) {
		return 0, false
	}
	return pid, true
}

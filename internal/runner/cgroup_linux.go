//go:build linux

package runner

import (
	"fmt"
	"math"
	"os/exec"
	"syscall"
)

func attachCgroup(cmd *exec.Cmd, fd uintptr) error {
	if fd > math.MaxInt32 {
		return fmt.Errorf("cgroup descriptor %d out of range", fd)
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.UseCgroupFD = true
	cmd.SysProcAttr.CgroupFD = int(fd)
	return nil
}

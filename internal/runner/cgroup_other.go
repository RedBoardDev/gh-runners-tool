//go:build !linux

package runner

import (
	"errors"
	"os/exec"
)

var errCgroupUnsupported = errors.New("cgroups are only supported on linux")

func attachCgroup(*exec.Cmd, uintptr) error {
	return errCgroupUnsupported
}

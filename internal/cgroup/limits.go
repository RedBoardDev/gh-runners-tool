package cgroup

import (
	"errors"
	"strings"
)

type Limits struct {
	MemoryMax  int64
	MemoryHigh int64
	CPUWeight  int
}

var (
	ErrUnavailable  = errors.New("cgroup v2 delegation unavailable")
	ErrInvalidName  = errors.New("invalid cgroup name")
	ErrNotSetUp     = errors.New("cgroup manager not set up")
	ErrNotEnforcing = errors.New("cgroup enforcement not active")
)

const delegationHint = "run ghr under a systemd service with Delegate=yes and OOMPolicy=continue"

func validName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00") {
		return ErrInvalidName
	}
	return nil
}

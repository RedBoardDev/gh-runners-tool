package model

import "time"

type Group struct {
	Name        string
	MaxRunners  int
	MinRunners  int
	Labels      []string
	RunnerGroup string
}

type RunnerInstance struct {
	ID      string
	Name    string
	Group   string
	WorkDir string
	Version string
}

type RunnerSnapshot struct {
	Name      string
	Group     string
	State     string
	PID       int
	StartedAt time.Time
	JobName   string
	JobID     string
}

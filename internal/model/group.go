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
	Name      string    `json:"name"`
	Group     string    `json:"group"`
	State     string    `json:"state"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
	JobName   string    `json:"job_name"`
	JobID     string    `json:"job_id"`
}

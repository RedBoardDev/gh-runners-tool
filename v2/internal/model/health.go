package model

import "time"

type GroupHealthStatus struct {
	Actual  int
	Desired int
	Max     int
	Min     int
	Healthy bool
	Issues  []HealthIssue
}

type HealthIssue struct {
	Level      EventLevel
	Type       string
	Group      string
	Runner     string
	Message    string
	DetectedAt time.Time
}

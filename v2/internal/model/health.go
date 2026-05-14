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
	Level      EventLevel `json:"level"`
	Type       string     `json:"type"`
	Group      string     `json:"group"`
	Runner     string     `json:"runner"`
	Message    string     `json:"message"`
	DetectedAt time.Time  `json:"detected_at"`
}

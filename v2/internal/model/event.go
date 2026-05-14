package model

import "time"

type EventLevel string

const (
	LevelInfo     EventLevel = "info"
	LevelWarning  EventLevel = "warning"
	LevelError    EventLevel = "error"
	LevelCritical EventLevel = "critical"
)

type Event struct {
	Type      string
	Level     EventLevel
	Group     string
	Runner    string
	Message   string
	Details   map[string]string
	Timestamp time.Time
}

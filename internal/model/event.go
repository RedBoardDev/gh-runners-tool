package model

import "time"

type EventLevel string

const (
	LevelInfo     EventLevel = "info"
	LevelWarning  EventLevel = "warning"
	LevelError    EventLevel = "error"
	LevelCritical EventLevel = "critical"
)

const (
	EventDaemonStart = "daemon.start"
	EventDaemonStop  = "daemon.stop"
	EventDaemonCrash = "daemon.crash"

	EventGroupCreated   = "group.created"
	EventGroupDeleted   = "group.deleted"
	EventGroupScaleUp   = "group.scale_up"
	EventGroupScaleDown = "group.scale_down"

	EventRunnerStarted   = "runner.started"
	EventRunnerCompleted = "runner.completed"
	EventRunnerFailed    = "runner.failed"
	EventRunnerTimeout   = "runner.timeout"

	EventHealthZombieRunner      = "health.zombie_runner"
	EventHealthRunnerTimeout     = "health.runner_timeout"
	EventHealthGroupDegraded     = "health.group_degraded"
	EventHealthGroupDisconnected = "health.group_disconnected"
	EventHealthGroupFailing      = "health.group_failing"
	EventHealthDiskLow           = "health.disk_low"
	EventHealthOrphanKilled      = "health.orphan_killed"
	EventHealthIdleTimeout       = "health.idle_timeout"
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

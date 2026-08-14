package health

import (
	"fmt"
	"syscall"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/logging"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
)

func (m *Monitor) checkDiskSpace() {
	if m.cfg.MinDiskSpace <= 0 {
		return
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		m.logger.Warn("failed to check disk space", logging.KeyError, err)
		return
	}

	//nolint:unconvert // Bsize is int64 on linux but uint32 on darwin; drop the conversion and the darwin build stops compiling.
	available := int64(stat.Bavail) * int64(stat.Bsize)
	if available < m.cfg.MinDiskSpace {
		m.issues = append(m.issues, model.HealthIssue{
			Level:      model.LevelWarning,
			Type:       model.EventHealthDiskLow,
			Group:      "",
			Runner:     "",
			Message:    fmt.Sprintf("available disk space %d bytes is below minimum %d bytes", available, m.cfg.MinDiskSpace),
			DetectedAt: time.Now(),
		})
	}
}

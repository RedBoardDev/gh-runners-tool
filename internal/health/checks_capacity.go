package health

import (
	"fmt"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
)

func (m *Monitor) reportWaitingForCapacity(group string, deferred int) {
	if deferred <= 0 {
		return
	}
	m.issues = append(m.issues, model.HealthIssue{
		Level:      model.LevelInfo,
		Type:       model.EventHealthGroupWaitingCapacity,
		Group:      group,
		Message:    fmt.Sprintf("group %s is waiting for capacity: %d runner(s) deferred", group, deferred),
		DetectedAt: time.Now(),
	})
}

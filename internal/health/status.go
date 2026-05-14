package health

import (
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
)

type HealthStatus struct {
	LastCheck time.Time
	Issues    []model.HealthIssue
}

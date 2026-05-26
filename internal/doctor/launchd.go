package doctor

import (
	"context"
	"fmt"
	"os"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/launchd"
)

type LaunchdCheck struct {
	Label     string
	PlistPath string
}

func (c LaunchdCheck) Name() string { return "launchd" }

func (c LaunchdCheck) Run(_ context.Context) Result {
	res := Result{Name: c.Name(), Details: []string{"label: " + c.Label}}

	if _, err := os.Stat(c.PlistPath); err != nil {
		if os.IsNotExist(err) {
			res.Status = StatusWarn
			res.Summary = "service not installed"
			res.Hint = "run 'ghr start' to register the launchd service"
			return res
		}
		res.Status = StatusFail
		res.Summary = fmt.Sprintf("stat plist: %v", err)
		res.Details = append(res.Details, c.PlistPath)
		return res
	}
	res.Details = append(res.Details, "plist: "+c.PlistPath)

	pid, running := launchd.Status(c.Label)
	if !running {
		res.Status = StatusWarn
		res.Summary = "service installed but not running"
		res.Hint = "run 'ghr start' to load it"
		return res
	}

	res.Status = StatusOK
	res.Summary = fmt.Sprintf("running (pid %d)", pid)
	return res
}

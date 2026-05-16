package doctor

import (
	"context"
	"fmt"
	"syscall"
)

type DiskCheck struct {
	Paths   []string
	MinFree int64
}

func (c DiskCheck) Name() string { return "disk" }

func (c DiskCheck) Run(_ context.Context) Result {
	res := Result{Name: c.Name()}

	seen := map[string]bool{}
	worst := Status(StatusOK)
	for _, p := range c.Paths {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true

		var stat syscall.Statfs_t
		if err := syscall.Statfs(p, &stat); err != nil {
			res.Details = append(res.Details, fmt.Sprintf("%s: statfs error: %v", p, err))
			if worst < StatusWarn {
				worst = StatusWarn
			}
			continue
		}
		available := int64(stat.Bavail) * int64(stat.Bsize)
		res.Details = append(res.Details, fmt.Sprintf("%s: %s free", p, humanBytes(available)))
		if c.MinFree > 0 && available < c.MinFree {
			if worst < StatusFail {
				worst = StatusFail
			}
		}
	}

	if len(res.Details) == 0 {
		res.Status = StatusSkip
		res.Summary = "no paths configured"
		return res
	}

	res.Status = worst
	switch worst {
	case StatusOK:
		res.Summary = "all paths above minimum"
	case StatusWarn:
		res.Summary = "could not stat one or more paths"
	case StatusFail:
		res.Summary = fmt.Sprintf("free space below minimum (%s)", humanBytes(c.MinFree))
		res.Hint = "free disk space or move state/cache dirs to a larger volume"
	}
	return res
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

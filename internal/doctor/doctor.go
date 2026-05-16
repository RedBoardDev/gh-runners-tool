package doctor

import (
	"context"
	"sort"
	"sync"
	"time"
)

type Status int

const (
	StatusOK Status = iota
	StatusWarn
	StatusFail
	StatusSkip
)

func (s Status) String() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusWarn:
		return "warn"
	case StatusFail:
		return "fail"
	case StatusSkip:
		return "skip"
	}
	return "unknown"
}

type Result struct {
	Name    string
	Status  Status
	Summary string
	Details []string
	Hint    string
	Elapsed time.Duration
}

type Check interface {
	Name() string
	Run(ctx context.Context) Result
}

type Report struct {
	Results []Result
}

func (r Report) ExitCode() int {
	for _, res := range r.Results {
		if res.Status == StatusFail {
			return 1
		}
	}
	return 0
}

func (r Report) Counts() (ok, warn, fail, skip int) {
	for _, res := range r.Results {
		switch res.Status {
		case StatusOK:
			ok++
		case StatusWarn:
			warn++
		case StatusFail:
			fail++
		case StatusSkip:
			skip++
		}
	}
	return
}

func Run(ctx context.Context, checks []Check, perCheckTimeout time.Duration) Report {
	results := make([]Result, len(checks))
	var wg sync.WaitGroup
	for i, c := range checks {
		wg.Add(1)
		go func(idx int, chk Check) {
			defer wg.Done()
			results[idx] = runOne(ctx, chk, perCheckTimeout)
		}(i, c)
	}
	wg.Wait()

	sort.SliceStable(results, func(i, j int) bool {
		return indexOf(checks, results[i].Name) < indexOf(checks, results[j].Name)
	})
	return Report{Results: results}
}

func runOne(ctx context.Context, c Check, timeout time.Duration) Result {
	start := time.Now()
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	done := make(chan Result, 1)
	go func() { done <- c.Run(cctx) }()

	select {
	case res := <-done:
		if res.Name == "" {
			res.Name = c.Name()
		}
		res.Elapsed = time.Since(start)
		return res
	case <-cctx.Done():
		return Result{
			Name:    c.Name(),
			Status:  StatusFail,
			Summary: "timed out",
			Hint:    "check is hanging; rerun with a longer timeout or investigate the underlying resource",
			Elapsed: time.Since(start),
		}
	}
}

func indexOf(checks []Check, name string) int {
	for i, c := range checks {
		if c.Name() == name {
			return i
		}
	}
	return len(checks)
}

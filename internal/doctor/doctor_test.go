package doctor

import (
	"context"
	"testing"
	"time"
)

type stubCheck struct {
	name string
	out  Result
	wait time.Duration
}

func (s stubCheck) Name() string { return s.name }
func (s stubCheck) Run(ctx context.Context) Result {
	if s.wait > 0 {
		select {
		case <-time.After(s.wait):
		case <-ctx.Done():
			return Result{Status: StatusFail, Summary: "ctx canceled"}
		}
	}
	return s.out
}

func TestRun_PreservesOrderAndReportsAll(t *testing.T) {
	checks := []Check{
		stubCheck{name: "a", out: Result{Status: StatusOK, Summary: "a"}},
		stubCheck{name: "b", out: Result{Status: StatusWarn, Summary: "b"}},
		stubCheck{name: "c", out: Result{Status: StatusFail, Summary: "c"}},
		stubCheck{name: "d", out: Result{Status: StatusSkip, Summary: "d"}},
	}
	r := Run(context.Background(), checks, time.Second)
	if len(r.Results) != 4 {
		t.Fatalf("got %d results, want 4", len(r.Results))
	}
	for i, want := range []string{"a", "b", "c", "d"} {
		if r.Results[i].Name != want {
			t.Errorf("pos %d: got %q want %q", i, r.Results[i].Name, want)
		}
	}
}

func TestReport_ExitCode(t *testing.T) {
	cases := []struct {
		name   string
		res    []Result
		expect int
	}{
		{"all_ok", []Result{{Status: StatusOK}, {Status: StatusOK}}, 0},
		{"warn_only_is_zero", []Result{{Status: StatusOK}, {Status: StatusWarn}}, 0},
		{"single_fail_is_one", []Result{{Status: StatusOK}, {Status: StatusFail}}, 1},
		{"skip_only_is_zero", []Result{{Status: StatusSkip}}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Report{Results: tc.res}
			if got := r.ExitCode(); got != tc.expect {
				t.Errorf("got %d want %d", got, tc.expect)
			}
		})
	}
}

func TestRun_TimeoutMarksCheckFailed(t *testing.T) {
	checks := []Check{
		stubCheck{name: "slow", out: Result{Status: StatusOK}, wait: 200 * time.Millisecond},
	}
	r := Run(context.Background(), checks, 20*time.Millisecond)
	if r.Results[0].Status != StatusFail {
		t.Fatalf("expected timed-out check to be FAIL, got %s", r.Results[0].Status)
	}
	if r.Results[0].Summary != "timed out" {
		t.Errorf("summary = %q, want %q", r.Results[0].Summary, "timed out")
	}
}

func TestRun_FailureInOneDoesNotBlockOthers(t *testing.T) {
	checks := []Check{
		stubCheck{name: "slow", out: Result{Status: StatusOK}, wait: 50 * time.Millisecond},
		stubCheck{name: "fast", out: Result{Status: StatusOK}},
	}
	start := time.Now()
	r := Run(context.Background(), checks, time.Second)
	elapsed := time.Since(start)
	if elapsed > 200*time.Millisecond {
		t.Errorf("checks ran sequentially (elapsed %s)", elapsed)
	}
	if len(r.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(r.Results))
	}
}

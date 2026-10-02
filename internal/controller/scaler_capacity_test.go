package controller

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/capacity"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/config"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/runner"
	"github.com/actions/scaleset"
)

const gib = int64(1) << 30

var epoch = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

type scriptedProcess struct {
	mu          sync.Mutex
	startErrs   []error
	prepareErr  error
	startCalls  int
	started     []string
	stopped     []string
	cleaned     []string
	startedChan chan string
}

func (p *scriptedProcess) Prepare(_ context.Context, _ *model.RunnerInstance, _ string) (string, error) {
	return "/tmp/workdir", p.prepareErr
}

func (p *scriptedProcess) Start(_ context.Context, instance *model.RunnerInstance, _, _ string, _ io.Writer) (*runner.Process, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	call := p.startCalls
	p.startCalls++
	if call < len(p.startErrs) && p.startErrs[call] != nil {
		return nil, p.startErrs[call]
	}
	p.started = append(p.started, instance.Name)
	if p.startedChan != nil {
		p.startedChan <- instance.Name
	}
	return &runner.Process{
		Name:      instance.Name,
		Group:     instance.Group,
		PID:       int32(1000 + len(p.started)),
		StartedAt: epoch.Add(time.Duration(len(p.started)) * time.Second),
	}, nil
}

func (p *scriptedProcess) Stop(_ context.Context, proc *runner.Process) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopped = append(p.stopped, proc.Name)
	return nil
}

func (p *scriptedProcess) Cleanup(proc *runner.Process) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cleaned = append(p.cleaned, proc.Name)
	return nil
}

func (p *scriptedProcess) stopCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.stopped)
}

type capacityFixture struct {
	t       *testing.T
	alloc   *capacity.Allocator
	process *scriptedProcess
	client  *fakeScaleSetClient
	scaler  *MacOSScaler
}

func newCapacityFixture(t *testing.T, total capacity.Resources, minRunners, maxRunners int, reserve capacity.Resources, extra ...capacity.GroupSpec) *capacityFixture {
	t.Helper()
	specs := append([]capacity.GroupSpec{{Name: "g", MinRunners: minRunners, Reserve: reserve}}, extra...)
	alloc := capacity.New(total, specs, slog.New(slog.DiscardHandler))
	return newFixtureWithBudget(t, alloc, minRunners, maxRunners)
}

func newFixtureWithBudget(t *testing.T, budget capacityBudget, minRunners, maxRunners int) *capacityFixture {
	t.Helper()
	process := &scriptedProcess{}
	client := &fakeScaleSetClient{jitID: 7}
	scaler := &MacOSScaler{
		client:     client,
		process:    process,
		logMgr:     tempLogMgr(t),
		notifier:   &mockNotifier{},
		groupName:  "g",
		minRunners: minRunners,
		maxRunners: maxRunners,
		logger:     testLogger(),
		idle:       make(map[string]*runner.Process),
		busy:       make(map[string]*runner.Process),
		budget:     budget,
	}
	f := &capacityFixture{t: t, process: process, client: client, scaler: scaler}
	if alloc, ok := budget.(*capacity.Allocator); ok {
		f.alloc = alloc
	}
	return f
}

func (f *capacityFixture) handleCount(count int) int {
	f.t.Helper()
	got, err := f.scaler.HandleDesiredRunnerCount(context.Background(), count)
	if err != nil {
		f.t.Fatalf("HandleDesiredRunnerCount(%d): %v", count, err)
	}
	return got
}

func (f *capacityFixture) runnerNames() []string {
	var names []string
	for _, snap := range f.scaler.Snapshots() {
		names = append(names, snap.Name)
	}
	return names
}

func (f *capacityFixture) snapshotNames(state string) []string {
	var names []string
	for _, snap := range f.scaler.Snapshots() {
		if snap.State == state {
			names = append(names, snap.Name)
		}
	}
	return names
}

func (f *capacityFixture) startJob(name string) {
	f.t.Helper()
	if err := f.scaler.HandleJobStarted(context.Background(), &scaleset.JobStarted{RunnerName: name}); err != nil {
		f.t.Fatalf("HandleJobStarted: %v", err)
	}
}

func (f *capacityFixture) completeJob(name string) {
	f.t.Helper()
	err := f.scaler.HandleJobCompleted(context.Background(), &scaleset.JobCompleted{RunnerName: name, Result: "succeeded"})
	if err != nil {
		f.t.Fatalf("HandleJobCompleted: %v", err)
	}
}

func (f *capacityFixture) reserved() int {
	return f.alloc.Status().Groups["g"].Reserved
}

func (f *capacityFixture) waiting() int {
	return f.alloc.WaitingRunners()["g"]
}

func reserve(memoryGiB int64, cpus float64) capacity.Resources {
	return capacity.Resources{MemoryBytes: memoryGiB * gib, CPUMilli: int64(cpus * 1000)}
}

func TestReconcile_DenialYieldsFewerRunnersAndRecordsWaiting(t *testing.T) {
	tests := []struct {
		name        string
		total       capacity.Resources
		reserve     capacity.Resources
		count       int
		wantRunners int
		wantWaiting int
	}{
		{"memory limits the group", reserve(8, 100), reserve(4, 1), 5, 2, 3},
		{"cpus limit the group", reserve(100, 6), reserve(1, 3), 4, 2, 2},
		{"everything fits", reserve(100, 100), reserve(4, 1), 3, 3, 0},
		{"max_runners caps before capacity does", reserve(100, 100), reserve(4, 1), 9, 5, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCapacityFixture(t, tt.total, 0, 5, tt.reserve)

			if got := f.handleCount(tt.count); got != tt.wantRunners {
				t.Fatalf("HandleDesiredRunnerCount returned %d runners, want %d", got, tt.wantRunners)
			}
			if got := f.waiting(); got != tt.wantWaiting {
				t.Errorf("waiting = %d, want %d", got, tt.wantWaiting)
			}
			if got := f.reserved(); got != tt.wantRunners {
				t.Errorf("reserved leases = %d, want %d", got, tt.wantRunners)
			}
		})
	}
}

func TestWake_ProvisionsAfterRelease(t *testing.T) {
	f := newCapacityFixture(t, reserve(8, 100), 0, 5, reserve(4, 1))
	f.handleCount(5)
	first := f.runnerNames()[0]

	f.completeJob(first)

	if got := f.reserved(); got != 1 {
		t.Fatalf("reserved after completion = %d, want 1", got)
	}
	f.scaler.Wake(context.Background())

	if got := len(f.runnerNames()); got != 2 {
		t.Fatalf("runners after wake = %d, want 2 (one slot freed)", got)
	}
	if got := f.waiting(); got != 3 {
		t.Errorf("waiting after wake = %d, want 3", got)
	}
}

func TestWake_ReleaseFromAnotherGroupReachesWaiterThroughRunLoop(t *testing.T) {
	alloc := capacity.New(reserve(8, 100), []capacity.GroupSpec{
		{Name: "g", Reserve: reserve(4, 1)},
		{Name: "other", Reserve: reserve(4, 1)},
	}, slog.New(slog.DiscardHandler))

	waiter := newFixtureWithBudget(t, alloc, 0, 4)
	waiter.process.startedChan = make(chan string, 8)
	alloc.Register("g", waiter.scaler)

	held := alloc.Acquire("other", 2)
	if len(held.Leases) != 2 {
		t.Fatalf("setup: other got %d leases", len(held.Leases))
	}
	waiter.handleCount(1)
	if len(waiter.runnerNames()) != 0 || waiter.waiting() != 1 {
		t.Fatalf("setup: waiter should be starved, runners=%v waiting=%d", waiter.runnerNames(), waiter.waiting())
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- alloc.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	held.Leases[0].Release()

	select {
	case <-waiter.process.startedChan:
	case <-time.After(5 * time.Second):
		t.Fatal("waiting scaler was not woken to provision after a release")
	}
	if waiter.waiting() != 0 {
		t.Errorf("waiting = %d after provisioning, want 0", waiter.waiting())
	}
}

func TestStartRunner_FailureReleasesLease(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f *capacityFixture)
	}{
		{"jit config failure", func(f *capacityFixture) { f.client.jitErr = errors.New("jit down") }},
		{"prepare failure", func(f *capacityFixture) { f.process.prepareErr = errors.New("disk full") }},
		{"process start failure", func(f *capacityFixture) { f.process.startErrs = []error{errors.New("exec failed")} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCapacityFixture(t, reserve(8, 8), 0, 5, reserve(4, 4))
			tt.setup(f)

			if got := f.handleCount(1); got != 0 {
				t.Fatalf("runners = %d, want 0 after a failed start", got)
			}
			if got := f.reserved(); got != 0 {
				t.Errorf("leases leaked after failed start: reserved = %d", got)
			}
			if used := f.alloc.Status().Used; !used.IsZero() {
				t.Errorf("capacity leaked after failed start: used = %+v", used)
			}
			if got := f.waiting(); got != 0 {
				t.Errorf("a failed start is not a capacity wait, waiting = %d", got)
			}
		})
	}
}

type channelWaker struct {
	woken chan<- struct{}
}

func (w channelWaker) Wake(context.Context) { w.woken <- struct{}{} }

func TestStartRunner_FailureDoesNotWakeOtherGroups(t *testing.T) {
	alloc := capacity.New(reserve(8, 8), []capacity.GroupSpec{
		{Name: "g", Priority: 100, Reserve: reserve(4, 4)},
		{Name: "starved", Reserve: reserve(8, 8)},
		{Name: "hog", Priority: 200, Reserve: reserve(4, 4)},
	}, slog.New(slog.DiscardHandler))
	f := newFixtureWithBudget(t, alloc, 0, 5)
	f.client.jitErr = errors.New("jit down")

	woken := make(chan struct{}, 4)
	alloc.Register("starved", channelWaker{woken: woken})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- alloc.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	hog := alloc.Acquire("hog", 1)
	if len(hog.Leases) != 1 {
		t.Fatalf("setup: hog got %d leases", len(hog.Leases))
	}
	if grant := alloc.Acquire("starved", 1); grant.Waiting != 1 {
		t.Fatalf("setup: starved should be waiting, got %+v", grant)
	}

	f.handleCount(1)
	if got := f.reserved(); got != 0 {
		t.Fatalf("setup: the failed start must have given its lease back, reserved = %d", got)
	}

	select {
	case <-woken:
		t.Fatal("a failed start woke waiters: failing starts would retry in a hot loop")
	case <-time.After(150 * time.Millisecond):
	}

	hog.Leases[0].Release()
	select {
	case <-woken:
	case <-time.After(5 * time.Second):
		t.Fatal("control: releasing a real runner must wake the waiting group")
	}
}

func TestHandleJobCompleted_ReleasesLeaseExactlyOnce(t *testing.T) {
	f := newCapacityFixture(t, reserve(8, 8), 0, 5, reserve(4, 4))
	f.handleCount(2)
	if got := f.reserved(); got != 2 {
		t.Fatalf("setup: reserved = %d, want 2", got)
	}
	names := f.runnerNames()
	f.startJob(names[0])

	f.completeJob(names[0])
	f.completeJob(names[0])
	f.completeJob("never-seen")

	if got := f.reserved(); got != 1 {
		t.Fatalf("reserved = %d, want 1 (only the completed runner released, once)", got)
	}
	if used := f.alloc.Status().Used; used != reserve(4, 4) {
		t.Fatalf("used = %+v, want exactly one runner's reservation", used)
	}
	if grant := f.alloc.Acquire("g", 5); len(grant.Leases) != 1 {
		t.Errorf("a double release would over-admit, got %d leases, want 1", len(grant.Leases))
	}
}

func TestKillRunner_ReleasesLease(t *testing.T) {
	f := newCapacityFixture(t, reserve(8, 8), 0, 5, reserve(4, 4))
	f.handleCount(2)
	names := f.runnerNames()
	f.startJob(names[0])

	if err := f.scaler.killRunner(context.Background(), names[0]); err != nil {
		t.Fatalf("killRunner: %v", err)
	}
	if got := f.reserved(); got != 1 {
		t.Fatalf("reserved after killing a busy runner = %d, want 1", got)
	}
}

func TestKillIdleRunner_ReleasesLeaseOnlyWhenActuallyIdle(t *testing.T) {
	f := newCapacityFixture(t, reserve(8, 8), 0, 5, reserve(4, 4))
	f.handleCount(2)
	names := f.runnerNames()
	f.startJob(names[0])

	if err := f.scaler.killIdleRunner(context.Background(), names[0]); err != nil {
		t.Fatalf("killIdleRunner(busy): %v", err)
	}
	if got := f.reserved(); got != 2 {
		t.Fatalf("a busy runner must keep its reservation, reserved = %d", got)
	}

	var idleName string
	for _, name := range names {
		if name != names[0] {
			idleName = name
		}
	}
	if err := f.scaler.killIdleRunner(context.Background(), idleName); err != nil {
		t.Fatalf("killIdleRunner(idle): %v", err)
	}
	if got := f.reserved(); got != 1 {
		t.Fatalf("reserved after idle kill = %d, want 1", got)
	}
}

func TestShutdown_ReleasesEveryLease(t *testing.T) {
	f := newCapacityFixture(t, reserve(16, 16), 0, 5, reserve(4, 4))
	f.handleCount(3)
	f.startJob(f.runnerNames()[0])

	f.scaler.Shutdown(context.Background())

	if used := f.alloc.Status().Used; !used.IsZero() {
		t.Fatalf("capacity not fully released at shutdown: %+v", used)
	}
	if f.process.stopCount() != 3 {
		t.Errorf("stopped %d runners, want 3", f.process.stopCount())
	}
}

func TestReconcile_AfterShutdownProvisionsNothing(t *testing.T) {
	f := newCapacityFixture(t, reserve(16, 16), 0, 5, reserve(4, 4))
	f.handleCount(1)
	f.scaler.Shutdown(context.Background())

	f.scaler.Wake(context.Background())
	f.handleCount(3)

	if got := len(f.runnerNames()); got != 0 {
		t.Fatalf("a shut-down scaler provisioned %d runners", got)
	}
	if used := f.alloc.Status().Used; !used.IsZero() {
		t.Fatalf("a shut-down scaler holds capacity: %+v", used)
	}
}

func TestWake_BeforeAnyDesiredCountDoesNothing(t *testing.T) {
	f := newCapacityFixture(t, reserve(16, 16), 1, 5, reserve(4, 4))

	f.scaler.Wake(context.Background())

	if got := len(f.runnerNames()); got != 0 {
		t.Fatalf("Wake provisioned %d runners before the listener reported demand", got)
	}
}

func TestSurplusIdleTeardown(t *testing.T) {
	tests := []struct {
		name       string
		min        int
		firstCount int
		busy       int
		newCount   int
		wantIdle   int
		wantBusy   int
	}{
		{"queued jobs cancelled, idle drains to zero", 0, 3, 0, 0, 0, 0},
		{"minimum is kept idle", 2, 3, 0, 0, 2, 0},
		{"idle kept to cover remaining demand", 0, 4, 0, 2, 2, 0},
		{"busy runners are never trimmed", 0, 3, 2, 2, 0, 2},
		{"idle beyond demand is trimmed around busy ones", 1, 3, 1, 1, 1, 1},
		{"no surplus changes nothing", 0, 2, 0, 2, 2, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCapacityFixture(t, reserve(100, 100), tt.min, 10, reserve(1, 1))
			f.handleCount(tt.firstCount)
			for _, name := range f.runnerNames()[:tt.busy] {
				f.startJob(name)
			}

			f.handleCount(tt.newCount)

			if got := len(f.snapshotNames("idle")); got != tt.wantIdle {
				t.Errorf("idle = %d, want %d", got, tt.wantIdle)
			}
			if got := len(f.snapshotNames("busy")); got != tt.wantBusy {
				t.Errorf("busy = %d, want %d", got, tt.wantBusy)
			}
			if got := f.reserved(); got != tt.wantIdle+tt.wantBusy {
				t.Errorf("reserved = %d, want %d (trimmed runners must release)", got, tt.wantIdle+tt.wantBusy)
			}
		})
	}
}

func TestSurplusIdleTeardown_KeepsOldestRunners(t *testing.T) {
	f := newCapacityFixture(t, reserve(100, 100), 0, 10, reserve(1, 1))
	f.handleCount(4)
	oldest := f.process.started[0]

	f.handleCount(1)

	idle := f.snapshotNames("idle")
	if len(idle) != 1 || idle[0] != oldest {
		t.Fatalf("surviving idle runners = %v, want only the oldest %q", idle, oldest)
	}
}

func TestSurplusIdleTeardown_ReleasedCapacityServesWaitingGroup(t *testing.T) {
	alloc := capacity.New(reserve(4, 4), []capacity.GroupSpec{
		{Name: "g", Reserve: reserve(2, 2)},
		{Name: "other", Priority: 10, Reserve: reserve(2, 2)},
	}, slog.New(slog.DiscardHandler))
	f := newFixtureWithBudget(t, alloc, 0, 5)

	f.handleCount(2)
	if grant := alloc.Acquire("other", 1); len(grant.Leases) != 0 || grant.Waiting != 1 {
		t.Fatalf("setup: other should be starved, got %+v", grant)
	}

	f.handleCount(0)

	if grant := alloc.Acquire("other", 1); len(grant.Leases) != 1 {
		t.Fatalf("idle runners above demand kept holding capacity")
	}
}

func TestSurplusIdleTeardown_DisabledWithoutCapacity(t *testing.T) {
	f := newFixtureWithBudget(t, nil, 0, 10)

	f.handleCount(4)
	if got := len(f.snapshotNames("idle")); got != 4 {
		t.Fatalf("setup: idle = %d, want 4", got)
	}
	f.handleCount(0)

	if got := len(f.snapshotNames("idle")); got != 4 {
		t.Fatalf("idle = %d, want 4: without capacity configured nothing is torn down", got)
	}
	if f.process.stopCount() != 0 {
		t.Fatalf("stopped %d runners without capacity configured", f.process.stopCount())
	}
}

func TestReconcile_WithoutCapacityMatchesLegacyBehaviour(t *testing.T) {
	tests := []struct {
		name       string
		min        int
		max        int
		count      int
		failFirst  bool
		wantStarts int
	}{
		{"min plus demand", 1, 5, 2, false, 3},
		{"capped by max", 0, 3, 9, false, 3},
		{"min alone", 2, 5, 0, false, 2},
		{"one failing start does not stop the others", 0, 5, 3, true, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixtureWithBudget(t, nil, tt.min, tt.max)
			if tt.failFirst {
				f.process.startErrs = []error{errors.New("boom")}
			}

			got := f.handleCount(tt.count)

			if got != tt.wantStarts {
				t.Fatalf("runners = %d, want %d", got, tt.wantStarts)
			}
			if len(f.scaler.leases) != 0 {
				t.Errorf("no leases expected without capacity, got %d", len(f.scaler.leases))
			}
		})
	}
}

func TestReconcile_ZeroReserveGroupIgnoresStarvation(t *testing.T) {
	alloc := capacity.New(reserve(4, 4), []capacity.GroupSpec{
		{Name: "g"},
		{Name: "hog", Priority: 99, Reserve: reserve(4, 4)},
	}, slog.New(slog.DiscardHandler))
	f := newFixtureWithBudget(t, alloc, 0, 5)
	alloc.Acquire("hog", 1)
	alloc.Acquire("hog", 1)

	if got := f.handleCount(3); got != 3 {
		t.Fatalf("zero-reserve group started %d runners, want 3", got)
	}
}

func TestReconcile_ConcurrentWakeAndCountNeverExceedTarget(t *testing.T) {
	f := newCapacityFixture(t, reserve(100, 100), 0, 4, reserve(1, 1))

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := f.scaler.HandleDesiredRunnerCount(context.Background(), 4); err != nil {
				t.Errorf("HandleDesiredRunnerCount: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			f.scaler.Wake(context.Background())
		}()
	}
	wg.Wait()

	if got := len(f.runnerNames()); got != 4 {
		t.Fatalf("runners = %d, want exactly the target of 4", got)
	}
	if got := f.reserved(); got != 4 {
		t.Fatalf("reserved = %d, want 4", got)
	}
}

func TestScalerFor_ReusesScalerAcrossListenerRestartsOnlyWithCapacity(t *testing.T) {
	group := &config.GroupConfig{Name: "g", MaxRunners: 4}
	t.Run("with capacity the scaler and its leases survive", func(t *testing.T) {
		alloc := capacity.New(reserve(4, 4), nil, slog.New(slog.DiscardHandler))
		c := &GroupController{
			scalers:  make(map[string]*MacOSScaler),
			retained: make(map[string]*MacOSScaler),
			budget:   alloc,
			logger:   testLogger(),
		}

		first := c.scalerFor(group, 1, "/cache", testLogger())
		second := c.scalerFor(group, 2, "/cache", testLogger())

		if first != second {
			t.Fatal("a listener restart must reuse the scaler that owns the running runners")
		}
		if second.scaleSetID != 2 {
			t.Errorf("scaleSetID = %d, want the new session's 2", second.scaleSetID)
		}

		c.forgetScaler(group.Name)
		if third := c.scalerFor(group, 3, "/cache", testLogger()); third == first {
			t.Error("a forgotten scaler must not be reused")
		}
	})

	t.Run("without capacity every restart builds a fresh scaler as before", func(t *testing.T) {
		c := &GroupController{
			scalers:  make(map[string]*MacOSScaler),
			retained: make(map[string]*MacOSScaler),
			logger:   testLogger(),
		}

		first := c.scalerFor(group, 1, "/cache", testLogger())
		second := c.scalerFor(group, 1, "/cache", testLogger())

		if first == second {
			t.Fatal("without capacity the scaler must not be retained")
		}
	})
}

type hookClient struct {
	*fakeScaleSetClient
	onRemove func(id int) error
}

func (h *hookClient) RemoveRunner(ctx context.Context, id int) error {
	if err := h.onRemove(id); err != nil {
		return err
	}
	return h.fakeScaleSetClient.RemoveRunner(ctx, id)
}

func TestSurplusTrim_RemovesFromGitHubBeforeStopping(t *testing.T) {
	f := newCapacityFixture(t, reserve(100, 100), 0, 10, reserve(1, 1))
	var stoppedAtRemoval []int
	f.scaler.client = &hookClient{fakeScaleSetClient: f.client, onRemove: func(int) error {
		stoppedAtRemoval = append(stoppedAtRemoval, f.process.stopCount())
		return nil
	}}
	f.handleCount(2)

	f.handleCount(0)

	if len(stoppedAtRemoval) != 2 {
		t.Fatalf("RemoveRunner called %d times, want 2", len(stoppedAtRemoval))
	}
	for _, n := range stoppedAtRemoval {
		if n > 1 {
			t.Fatalf("a process was stopped before GitHub accepted its removal: %v", stoppedAtRemoval)
		}
	}
	if f.reserved() != 0 || len(f.runnerNames()) != 0 {
		t.Fatalf("runners should be gone after successful removal, reserved=%d", f.reserved())
	}
}

func TestSurplusTrim_KeepsRunnerGitHubRefusesToRemove(t *testing.T) {
	f := newCapacityFixture(t, reserve(100, 100), 0, 10, reserve(1, 1))
	f.scaler.client = &hookClient{fakeScaleSetClient: f.client, onRemove: func(int) error {
		return errors.New("runner is running a job")
	}}
	f.handleCount(2)

	f.handleCount(0)

	if got := len(f.snapshotNames("idle")); got != 2 {
		t.Fatalf("idle = %d, want both runners kept", got)
	}
	if f.reserved() != 2 {
		t.Fatalf("reserved = %d, a kept runner must keep its lease", f.reserved())
	}
	if f.process.stopCount() != 0 {
		t.Fatalf("stopped %d processes although GitHub refused removal", f.process.stopCount())
	}
}

func TestIdleRunnerThatExitsOnItsOwnReleasesItsLease(t *testing.T) {
	cache := t.TempDir()
	if err := os.WriteFile(filepath.Join(cache, "run.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	alloc := capacity.New(reserve(4, 4), []capacity.GroupSpec{{Name: "g", Reserve: reserve(4, 4)}}, slog.New(slog.DiscardHandler))
	f := newFixtureWithBudget(t, alloc, 0, 5)
	f.scaler.process = runner.NewProcessManager(t.TempDir(), testLogger())
	f.scaler.cachedDir = cache

	f.handleCount(1)

	deadline := time.After(10 * time.Second)
	for f.reserved() != 0 || len(f.runnerNames()) != 0 {
		select {
		case <-deadline:
			t.Fatalf("a runner that died on its own still holds its lease: reserved=%d", f.reserved())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

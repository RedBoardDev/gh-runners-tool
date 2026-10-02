package capacity

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

const gib = int64(1) << 30

func res(memoryGiB int64, cpus float64) Resources {
	return Resources{MemoryBytes: memoryGiB * gib, CPUMilli: int64(cpus * 1000)}
}

func spec(name string, priority, minRunners int, reserve Resources) GroupSpec {
	return GroupSpec{Name: name, Priority: priority, MinRunners: minRunners, Reserve: reserve}
}

func newAllocator(total Resources, specs ...GroupSpec) *Allocator {
	return New(total, specs, slog.New(slog.DiscardHandler))
}

func mustAcquire(t *testing.T, a *Allocator, group string, want, wantGranted, wantWaiting int) []*Lease {
	t.Helper()
	grant := a.Acquire(group, want)
	if len(grant.Leases) != wantGranted || grant.Waiting != wantWaiting {
		t.Fatalf("Acquire(%s, %d) = %d granted / %d waiting, want %d / %d",
			group, want, len(grant.Leases), grant.Waiting, wantGranted, wantWaiting)
	}
	return grant.Leases
}

func TestAcquire_AdmissionPerDimension(t *testing.T) {
	tests := []struct {
		name        string
		total       Resources
		reserve     Resources
		want        int
		wantGranted int
	}{
		{"memory is the binding dimension", res(10, 100), res(4, 1), 3, 2},
		{"cpus are the binding dimension", res(100, 10), res(1, 4), 3, 2},
		{"both fit", res(10, 10), res(2, 2), 2, 2},
		{"exact fit is admitted", res(10, 10), res(5, 5), 2, 2},
		{"one over exact fit is denied", res(10, 10), res(5, 5), 3, 2},
		{"reserve larger than capacity is never admitted", res(4, 4), res(5, 1), 1, 0},
		{"unlimited cpus ignores cpu reserve", Resources{MemoryBytes: 10 * gib}, res(4, 500), 2, 2},
		{"unlimited memory ignores memory reserve", Resources{CPUMilli: 10_000}, res(500, 4), 2, 2},
		{"want zero grants nothing", res(10, 10), res(1, 1), 0, 0},
		{"negative want grants nothing", res(10, 10), res(1, 1), -3, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newAllocator(tt.total, spec("g", 0, 0, tt.reserve))
			grant := a.Acquire("g", tt.want)
			if len(grant.Leases) != tt.wantGranted {
				t.Fatalf("granted = %d, want %d", len(grant.Leases), tt.wantGranted)
			}
			wantWaiting := max(tt.want, 0) - tt.wantGranted
			if grant.Waiting != wantWaiting {
				t.Fatalf("waiting = %d, want %d", grant.Waiting, wantWaiting)
			}
		})
	}
}

func TestAcquire_ZeroReserveGroupNeverBlocked(t *testing.T) {
	a := newAllocator(res(4, 4),
		spec("heavy", 100, 0, res(4, 4)),
		spec("free-rider", 0, 0, Resources{}),
	)

	held := mustAcquire(t, a, "heavy", 1, 1, 0)
	mustAcquire(t, a, "heavy", 1, 0, 1)

	mustAcquire(t, a, "free-rider", 5, 5, 0)

	if got := a.WaitingRunners(); len(got) != 1 || got["heavy"] != 1 {
		t.Fatalf("WaitingRunners() = %v, want only heavy waiting", got)
	}
	held[0].Release()
}

func TestAcquire_ZeroReserveGroupDoesNotBlockOthers(t *testing.T) {
	a := newAllocator(res(10, 10),
		spec("free-rider", 100, 0, Resources{}),
		spec("worker", 0, 0, res(2, 2)),
	)

	mustAcquire(t, a, "free-rider", 3, 3, 0)
	mustAcquire(t, a, "worker", 2, 2, 0)
}

func TestAcquire_UnknownGroupIsUnbounded(t *testing.T) {
	a := newAllocator(res(1, 1))
	mustAcquire(t, a, "stranger", 3, 3, 0)
}

func TestStrictPriority(t *testing.T) {
	a := newAllocator(res(10, 100),
		spec("low", 0, 0, res(3, 1)),
		spec("mid", 10, 0, res(1, 1)),
		spec("high", 50, 0, res(6, 1)),
	)

	lowLeases := mustAcquire(t, a, "low", 2, 2, 0)
	mustAcquire(t, a, "high", 1, 0, 1)

	t.Run("lower priority is denied while a higher one waits even though it fits", func(t *testing.T) {
		mustAcquire(t, a, "mid", 1, 0, 1)
	})

	lowLeases[0].Release()

	t.Run("higher priority is served first once capacity is released", func(t *testing.T) {
		mustAcquire(t, a, "high", 1, 1, 0)
	})

	t.Run("lower priority proceeds after the higher one is served", func(t *testing.T) {
		mustAcquire(t, a, "mid", 1, 1, 0)
	})
}

func TestStrictPriority_HigherPriorityWithoutDemandDoesNotBlock(t *testing.T) {
	a := newAllocator(res(10, 100),
		spec("high", 50, 0, res(6, 1)),
		spec("low", 0, 0, res(3, 1)),
	)

	mustAcquire(t, a, "low", 3, 3, 0)
}

func TestFIFOWithinPriority(t *testing.T) {
	a := newAllocator(res(6, 100),
		spec("holder", 0, 0, res(4, 1)),
		spec("first", 10, 0, res(4, 1)),
		spec("second", 10, 0, res(4, 1)),
	)

	held := mustAcquire(t, a, "holder", 1, 1, 0)
	mustAcquire(t, a, "first", 1, 0, 1)
	mustAcquire(t, a, "second", 1, 0, 1)

	held[0].Release()

	t.Run("newer waiter is denied although it asks first and fits", func(t *testing.T) {
		mustAcquire(t, a, "second", 1, 0, 1)
	})
	t.Run("oldest waiter is served", func(t *testing.T) {
		mustAcquire(t, a, "first", 1, 1, 0)
	})
	t.Run("newer waiter keeps waiting while capacity is short", func(t *testing.T) {
		mustAcquire(t, a, "second", 1, 0, 1)
	})
}

func TestFIFOWithinPriority_NewcomerQueuesBehindWaiter(t *testing.T) {
	a := newAllocator(res(6, 100),
		spec("holder", 0, 0, res(4, 1)),
		spec("waiter", 10, 0, res(4, 1)),
		spec("newcomer", 10, 0, res(1, 1)),
	)

	mustAcquire(t, a, "holder", 1, 1, 0)
	mustAcquire(t, a, "waiter", 1, 0, 1)
	mustAcquire(t, a, "newcomer", 1, 0, 1)
}

func TestMinRunnersAreProtectedFloors(t *testing.T) {
	a := newAllocator(res(10, 100),
		spec("steady", 0, 1, res(4, 1)),
		spec("burst", 50, 0, res(4, 1)),
	)

	t.Run("burst leaves room for another group's unfilled minimum", func(t *testing.T) {
		mustAcquire(t, a, "burst", 3, 1, 2)
	})

	t.Run("minimum runner is admitted although a higher priority group waits", func(t *testing.T) {
		mustAcquire(t, a, "steady", 1, 1, 0)
	})

	t.Run("runner beyond the minimum follows normal admission", func(t *testing.T) {
		mustAcquire(t, a, "steady", 1, 0, 1)
	})
}

func TestMinRunnersFloorShrinksAsItIsFilled(t *testing.T) {
	a := newAllocator(res(12, 100),
		spec("steady", 0, 2, res(3, 1)),
		spec("burst", 0, 0, res(3, 1)),
	)

	mustAcquire(t, a, "steady", 2, 2, 0)
	mustAcquire(t, a, "burst", 5, 2, 3)
}

func TestLease_DoubleReleaseIsHarmless(t *testing.T) {
	tests := []struct {
		name   string
		finish func(l *Lease)
	}{
		{"release twice", func(l *Lease) { l.Release(); l.Release() }},
		{"cancel twice", func(l *Lease) { l.Cancel(); l.Cancel() }},
		{"release then cancel", func(l *Lease) { l.Release(); l.Cancel() }},
		{"cancel then release", func(l *Lease) { l.Cancel(); l.Release() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newAllocator(res(8, 100), spec("g", 0, 0, res(4, 1)))

			leases := mustAcquire(t, a, "g", 1, 1, 0)
			tt.finish(leases[0])

			st := a.Status()
			if !st.Used.IsZero() || st.Groups["g"].Reserved != 0 {
				t.Fatalf("after release used=%+v reserved=%d, want zero", st.Used, st.Groups["g"].Reserved)
			}
			mustAcquire(t, a, "g", 3, 2, 1)
		})
	}
}

func TestLease_NilIsSafe(t *testing.T) {
	var l *Lease
	l.Release()
	l.Cancel()
}

func TestStatus(t *testing.T) {
	a := newAllocator(res(10, 8),
		spec("a", 5, 0, res(3, 2)),
		spec("b", 0, 0, res(4, 1)),
	)
	mustAcquire(t, a, "a", 2, 2, 0)
	mustAcquire(t, a, "b", 2, 1, 1)

	st := a.Status()
	if st.Total != res(10, 8) {
		t.Errorf("total = %+v", st.Total)
	}
	if st.Used != res(10, 5) {
		t.Errorf("used = %+v, want 10GiB / 5 cpus", st.Used)
	}
	if st.Free != res(0, 3) {
		t.Errorf("free = %+v, want 0 / 3 cpus", st.Free)
	}
	if g := st.Groups["a"]; g.Reserved != 2 || g.Waiting != 0 || g.Priority != 5 || g.Reserve != res(3, 2) {
		t.Errorf("group a = %+v", g)
	}
	if g := st.Groups["b"]; g.Reserved != 1 || g.Waiting != 1 {
		t.Errorf("group b = %+v", g)
	}
}

func TestStatus_UnlimitedDimensionReportsZeroFree(t *testing.T) {
	a := newAllocator(Resources{MemoryBytes: 10 * gib}, spec("g", 0, 0, res(1, 3)))
	mustAcquire(t, a, "g", 2, 2, 0)

	st := a.Status()
	if st.Free.CPUMilli != 0 || st.Used.CPUMilli != 6000 {
		t.Errorf("unlimited cpus: free=%d used=%d", st.Free.CPUMilli, st.Used.CPUMilli)
	}
	if st.Free.MemoryBytes != 8*gib {
		t.Errorf("free memory = %d, want 8GiB", st.Free.MemoryBytes)
	}
}

func TestWaitingRunners_ClearedWhenDemandDisappears(t *testing.T) {
	a := newAllocator(res(4, 4), spec("g", 0, 0, res(4, 4)))
	held := mustAcquire(t, a, "g", 1, 1, 0)
	mustAcquire(t, a, "g", 2, 0, 2)

	if got := a.WaitingRunners()["g"]; got != 2 {
		t.Fatalf("waiting = %d, want 2", got)
	}

	mustAcquire(t, a, "g", 0, 0, 0)
	if got := a.WaitingRunners(); len(got) != 0 {
		t.Fatalf("WaitingRunners() = %v, want empty after demand dropped", got)
	}
	held[0].Release()
}

func TestUnregister_StopsGroupFromBlockingOthers(t *testing.T) {
	a := newAllocator(res(10, 10),
		spec("high", 50, 0, res(8, 1)),
		spec("low", 0, 0, res(3, 1)),
		spec("holder", 0, 0, res(5, 1)),
	)
	mustAcquire(t, a, "holder", 1, 1, 0)
	mustAcquire(t, a, "high", 1, 0, 1)
	mustAcquire(t, a, "low", 1, 0, 1)

	a.Unregister("high")

	if got := a.WaitingRunners(); got["high"] != 0 {
		t.Fatalf("high should no longer wait, got %v", got)
	}
	mustAcquire(t, a, "low", 1, 1, 0)
}

func TestWaitingTransitionsAreLoggedOnce(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	a := New(res(4, 4), []GroupSpec{spec("g", 0, 0, res(4, 4))}, logger)

	held := mustAcquire(t, a, "g", 1, 1, 0)
	mustAcquire(t, a, "g", 1, 0, 1)
	mustAcquire(t, a, "g", 2, 0, 2)
	mustAcquire(t, a, "g", 1, 0, 1)

	if got := strings.Count(buf.String(), "group waiting for capacity"); got != 1 {
		t.Fatalf("start-waiting logged %d times, want 1:\n%s", got, buf.String())
	}
	for _, field := range []string{"group=g", "needed_per_runner=", "free="} {
		if !strings.Contains(buf.String(), field) {
			t.Errorf("log is missing %q:\n%s", field, buf.String())
		}
	}

	held[0].Release()
	mustAcquire(t, a, "g", 1, 1, 0)
	mustAcquire(t, a, "g", 0, 0, 0)

	if got := strings.Count(buf.String(), "group no longer waiting for capacity"); got != 1 {
		t.Fatalf("stop-waiting logged %d times, want 1:\n%s", got, buf.String())
	}
}

type recordingWaker struct {
	name  string
	woken chan<- string
}

func (w recordingWaker) Wake(context.Context) { w.woken <- w.name }

func TestWakeWaiting_PriorityOrderThenFIFO(t *testing.T) {
	a := newAllocator(res(4, 4),
		spec("holder", 100, 0, res(4, 4)),
		spec("early-low", 0, 0, res(4, 4)),
		spec("late-high", 10, 0, res(4, 4)),
		spec("early-high", 10, 0, res(4, 4)),
		spec("idle", 99, 0, res(4, 4)),
	)
	woken := make(chan string, 8)
	for _, name := range []string{"early-low", "late-high", "early-high", "idle"} {
		a.Register(name, recordingWaker{name: name, woken: woken})
	}

	mustAcquire(t, a, "holder", 1, 1, 0)
	mustAcquire(t, a, "early-low", 1, 0, 1)
	mustAcquire(t, a, "early-high", 1, 0, 1)
	mustAcquire(t, a, "late-high", 1, 0, 1)

	a.wakeWaiting(context.Background())
	close(woken)

	var order []string
	for name := range woken {
		order = append(order, name)
	}
	want := []string{"early-high", "late-high", "early-low"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("wake order = %v, want %v (groups without demand must not be woken)", order, want)
	}
}

func TestWakeWaiting_SkipsGroupsWithoutWaker(t *testing.T) {
	a := newAllocator(res(4, 4), spec("held", 0, 0, res(4, 4)), spec("waiter", 0, 0, res(4, 4)))
	mustAcquire(t, a, "held", 1, 1, 0)
	mustAcquire(t, a, "waiter", 1, 0, 1)

	a.wakeWaiting(context.Background())
}

func TestRun_ReleaseWakesWaitersButCancelDoesNot(t *testing.T) {
	a := newAllocator(res(8, 8),
		spec("holder", 0, 0, res(4, 4)),
		spec("waiter", 0, 0, res(4, 4)),
	)
	woken := make(chan string, 4)
	a.Register("waiter", recordingWaker{name: "waiter", woken: woken})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	held := mustAcquire(t, a, "holder", 2, 2, 0)
	mustAcquire(t, a, "waiter", 1, 0, 1)

	held[0].Cancel()
	select {
	case name := <-woken:
		t.Fatalf("Cancel must not wake waiters, woke %q", name)
	case <-time.After(100 * time.Millisecond):
	}

	held[1].Release()
	select {
	case <-woken:
	case <-time.After(2 * time.Second):
		t.Fatal("Release did not wake the waiting group")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop on context cancellation")
	}
}

func TestAcquire_ConcurrentUseNeverOvercommits(t *testing.T) {
	const workers = 16
	total := res(8, 100)
	a := newAllocator(total,
		spec("a", 0, 0, res(2, 1)),
		spec("b", 5, 1, res(3, 1)),
		spec("c", 0, 0, res(1, 1)),
	)

	var wg sync.WaitGroup
	for i := range workers {
		group := []string{"a", "b", "c"}[i%3]
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				grant := a.Acquire(group, 3)
				if used := a.Status().Used.MemoryBytes; used > total.MemoryBytes {
					t.Errorf("overcommitted: used %d > total %d", used, total.MemoryBytes)
					return
				}
				for _, l := range grant.Leases {
					l.Release()
				}
			}
		}()
	}
	wg.Wait()

	if st := a.Status(); !st.Used.IsZero() {
		t.Fatalf("leak: used = %+v after all releases", st.Used)
	}
}

func TestAcquire_UnregisteredGroupDoesNotRecordWaiting(t *testing.T) {
	a := newAllocator(res(10, 10),
		spec("high", 50, 0, res(8, 1)),
		spec("low", 0, 0, res(3, 1)),
		spec("holder", 0, 0, res(5, 1)),
	)
	a.Register("high", recordingWaker{name: "high", woken: make(chan string, 1)})
	mustAcquire(t, a, "holder", 1, 1, 0)
	a.Unregister("high")

	mustAcquire(t, a, "high", 1, 0, 1)

	if got := a.WaitingRunners(); got["high"] != 0 {
		t.Fatalf("an unregistered group must not be recorded as waiting, got %v", got)
	}
	mustAcquire(t, a, "low", 1, 1, 0)

	a.Register("high", recordingWaker{name: "high", woken: make(chan string, 1)})
	mustAcquire(t, a, "high", 1, 0, 1)
	if got := a.WaitingRunners(); got["high"] != 1 {
		t.Fatalf("a re-registered group records waiting again, got %v", got)
	}
}

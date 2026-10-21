package capacity

import (
	"cmp"
	"context"
	"maps"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func capped(s GroupSpec, maxRunners int) GroupSpec {
	s.MaxRunners = maxRunners
	return s
}

func TestCapacity(t *testing.T) {
	tests := []struct {
		name  string
		extra []GroupSpec
		max   int
		setup func(a *Allocator)
		group string
		want  int
	}{
		{
			name:  "free budget bounds the group",
			group: "g",
			want:  2,
		},
		{
			name:  "held runners count towards capacity",
			setup: func(a *Allocator) { a.Acquire("g", 1) },
			group: "g",
			want:  2,
		},
		{
			name:  "max_runners caps capacity",
			max:   1,
			group: "g",
			want:  1,
		},
		{
			name:  "zero reserve group is bounded by max_runners only",
			group: "free-rider",
			want:  7,
		},
		{
			name:  "unknown group advertises nothing",
			group: "stranger",
			want:  0,
		},
		{
			name:  "a waiting higher priority group leaves only what is held",
			extra: []GroupSpec{capped(spec("high", 50, 0, res(8, 1)), 10)},
			setup: func(a *Allocator) {
				a.Acquire("g", 1)
				a.Acquire("high", 2)
			},
			group: "g",
			want:  1,
		},
		{
			name:  "another group's unmet minimum is kept free",
			extra: []GroupSpec{capped(spec("steady", 0, 1, res(4, 1)), 10)},
			group: "g",
			want:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maxRunners := cmp.Or(tt.max, 10)
			specs := append([]GroupSpec{
				capped(spec("g", 0, 0, res(4, 1)), maxRunners),
				capped(spec("free-rider", 0, 0, Resources{}), 7),
			}, tt.extra...)
			a := newAllocator(res(10, 100), specs...)
			if tt.setup != nil {
				tt.setup(a)
			}
			if got := a.Capacity(tt.group); got != tt.want {
				t.Fatalf("Capacity(%s) = %d, want %d", tt.group, got, tt.want)
			}
		})
	}
}

func TestCapacity_SaturatedHostAdvertisesOnlyWarmRunners(t *testing.T) {
	a := newAllocator(res(8, 100),
		capped(spec("warm", 0, 2, res(1, 1)), 10),
		capped(spec("hog", 50, 0, res(6, 1)), 10),
	)
	mustAcquire(t, a, "warm", 2, 2, 0)
	mustAcquire(t, a, "hog", 1, 1, 0)

	if got := a.Capacity("warm"); got != 2 {
		t.Fatalf("Capacity(warm) = %d, want 2: warm runners must not attract jobs the host cannot add", got)
	}
}

func TestCapacity_UnmetMinimumIsAdmittedPastPriority(t *testing.T) {
	a := newAllocator(res(8, 100),
		capped(spec("steady", 0, 1, res(2, 1)), 5),
		capped(spec("high", 50, 0, res(8, 1)), 5),
	)
	mustAcquire(t, a, "high", 1, 0, 1)

	if got := a.Capacity("steady"); got != 1 {
		t.Fatalf("Capacity(steady) = %d, want 1 for its unmet minimum", got)
	}
}

func TestCapacity_ReservesNothing(t *testing.T) {
	a := newAllocator(res(10, 10), capped(spec("g", 0, 0, res(4, 4)), 5))
	a.Capacity("g")

	if st := a.Status(); !st.Used.IsZero() || st.Groups["g"].Reserved != 0 {
		t.Fatalf("Capacity changed the budget: %+v", st)
	}
	mustAcquire(t, a, "g", 2, 2, 0)
}

func advertisedExtras(a *Allocator) map[string]int {
	st := a.Status()
	extras := make(map[string]int, len(st.Groups))
	for name, g := range st.Groups {
		extras[name] = a.Capacity(name) - g.Reserved
	}
	return extras
}

func TestCapacity_WaitingDemandClaimsTheFreeRunnerFirst(t *testing.T) {
	a := newAllocator(res(8, 100),
		capped(spec("waiter", 0, 0, res(4, 1)), 5),
		capped(spec("idle", 50, 0, res(4, 1)), 5),
	)
	leases := mustAcquire(t, a, "waiter", 2, 2, 0)
	mustAcquire(t, a, "waiter", 1, 0, 1)
	leases[0].Release()

	got := advertisedExtras(a)
	if want := map[string]int{"waiter": 1, "idle": 0}; !maps.Equal(got, want) {
		t.Fatalf("advertised extras = %v, want %v: the one free runner goes to the job already waiting for it", got, want)
	}
}

func TestCapacity_IdleGroupsShareTheFreeBudget(t *testing.T) {
	a := newAllocator(res(16, 100),
		capped(spec("a", 0, 0, res(4, 1)), 10),
		capped(spec("b", 0, 0, res(4, 1)), 10),
	)

	got := advertisedExtras(a)
	if want := map[string]int{"a": 2, "b": 2}; !maps.Equal(got, want) {
		t.Fatalf("advertised extras = %v, want %v: four free runners must not be offered twice", got, want)
	}
}

func TestCapacity_LastFreeRunnerIsOfferedToEveryIdleGroup(t *testing.T) {
	a := newAllocator(res(8, 100),
		capped(spec("high", 50, 0, res(4, 1)), 5),
		capped(spec("low", 0, 0, res(4, 1)), 5),
	)
	mustAcquire(t, a, "high", 1, 1, 0)

	got := advertisedExtras(a)
	if want := map[string]int{"high": 1, "low": 1}; !maps.Equal(got, want) {
		t.Fatalf("advertised extras = %v, want %v: an idle higher group must not starve a lower one", got, want)
	}
}

func TestCapacity_SaturatedGroupsShareWhatIdleGroupsLeave(t *testing.T) {
	tests := []struct {
		name      string
		saturated []string
		want      map[string]int
	}{
		{
			name:      "one loaded group takes all but one runner per idle group",
			saturated: []string{"b"},
			want:      map[string]int{"a": 1, "b": 6, "c": 1},
		},
		{
			name:      "loaded groups split the budget",
			saturated: []string{"a", "b"},
			want:      map[string]int{"a": 4, "b": 3, "c": 1},
		},
		{
			name: "idle groups share the budget before any demand is known",
			want: map[string]int{"a": 3, "b": 3, "c": 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newAllocator(res(8, 100),
				capped(spec("a", 0, 0, res(1, 1)), 10),
				capped(spec("b", 0, 0, res(1, 1)), 10),
				capped(spec("c", 0, 0, res(1, 1)), 10),
			)
			for _, name := range tt.saturated {
				a.SetSaturated(name, true)
			}

			if got := advertisedExtras(a); !maps.Equal(got, tt.want) {
				t.Fatalf("advertised extras = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCapacity_SaturatedGroupCappedByMaxRunnersLeavesTheRestToIdleGroups(t *testing.T) {
	a := newAllocator(res(8, 100),
		capped(spec("idle", 0, 0, res(1, 1)), 10),
		capped(spec("loaded", 0, 0, res(1, 1)), 3),
	)
	a.SetSaturated("loaded", true)

	got := advertisedExtras(a)
	if want := map[string]int{"idle": 5, "loaded": 3}; !maps.Equal(got, want) {
		t.Fatalf("advertised extras = %v, want %v", got, want)
	}
}

func TestCapacity_LastFreeRunnerStaysWithTheIdleGroup(t *testing.T) {
	a := newAllocator(res(4, 100),
		capped(spec("idle", 0, 0, res(1, 1)), 10),
		capped(spec("loaded", 0, 0, res(1, 1)), 10),
	)
	a.SetSaturated("loaded", true)
	mustAcquire(t, a, "loaded", 3, 3, 0)

	got := advertisedExtras(a)
	if want := map[string]int{"idle": 1, "loaded": 1}; !maps.Equal(got, want) {
		t.Fatalf("advertised extras = %v, want %v: the idle group keeps the free runner, the loaded one only its anti-starvation runner", got, want)
	}
}

func TestCapacity_ListenerRestartForgetsSaturation(t *testing.T) {
	a := newAllocator(res(8, 100),
		capped(spec("a", 0, 0, res(1, 1)), 10),
		capped(spec("b", 0, 0, res(1, 1)), 10),
	)
	a.SetSaturated("b", true)
	a.Unregister("b")
	a.Register("b", nil)

	if got, want := advertisedExtras(a), map[string]int{"a": 4, "b": 4}; !maps.Equal(got, want) {
		t.Fatalf("advertised extras = %v, want %v", got, want)
	}
}

func TestCapacity_ClosedGroupLeavesItsShareToOthers(t *testing.T) {
	a := newAllocator(res(16, 100),
		capped(spec("a", 0, 0, res(4, 1)), 10),
		capped(spec("b", 0, 0, res(4, 1)), 10),
	)
	a.Unregister("b")

	if got := a.Capacity("a"); got != 4 {
		t.Fatalf("Capacity(a) = %d, want 4 while b has no listener", got)
	}
}

type capacityWatcher struct {
	alloc *Allocator
	group string
	last  atomic.Int64
}

func (w *capacityWatcher) Wake(context.Context) {}

func (w *capacityWatcher) CapacityChanged() {
	w.last.Store(int64(w.alloc.Capacity(w.group)))
}

func TestRun_CapacityChangesReachEveryGroupUnderChurn(t *testing.T) {
	a := newAllocator(res(12, 100),
		capped(spec("a", 0, 0, res(2, 1)), 4),
		capped(spec("b", 10, 1, res(3, 1)), 4),
		capped(spec("c", 5, 0, res(4, 1)), 4),
	)
	watchers := map[string]*capacityWatcher{}
	for _, name := range []string{"a", "b", "c"} {
		w := &capacityWatcher{alloc: a, group: name}
		w.last.Store(-1)
		watchers[name] = w
		a.Register(name, w)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	var held sync.Mutex
	var kept []*Lease
	var wg sync.WaitGroup
	for i := range 12 {
		group := []string{"a", "b", "c"}[i%3]
		wg.Go(func() {
			for j := range 300 {
				grant := a.Acquire(group, 2)
				if j%7 == 0 && len(grant.Leases) > 0 {
					held.Lock()
					kept = append(kept, grant.Leases[0])
					held.Unlock()
					grant.Leases = grant.Leases[1:]
				}
				for _, l := range grant.Leases {
					l.Release()
				}
			}
		})
	}
	wg.Wait()
	waitAdvertised(t, a, watchers)

	for _, l := range kept {
		l.Release()
	}
	waitAdvertised(t, a, watchers)
}

func waitAdvertised(t *testing.T, a *Allocator, watchers map[string]*capacityWatcher) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for name, w := range watchers {
		want := int64(a.Capacity(name))
		for w.last.Load() != want {
			if time.Now().After(deadline) {
				t.Fatalf("group %s last saw capacity %d, current is %d", name, w.last.Load(), want)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}

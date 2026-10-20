package capacity

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCapacity(t *testing.T) {
	tests := []struct {
		name  string
		extra []GroupSpec
		setup func(a *Allocator)
		group string
		limit int
		want  int
	}{
		{
			name:  "free budget bounds the group",
			group: "g",
			limit: 10,
			want:  2,
		},
		{
			name:  "held runners count towards capacity",
			setup: func(a *Allocator) { a.Acquire("g", 1) },
			group: "g",
			limit: 10,
			want:  2,
		},
		{
			name:  "limit caps capacity",
			group: "g",
			limit: 1,
			want:  1,
		},
		{
			name:  "zero reserve group is bounded by limit only",
			group: "free-rider",
			limit: 7,
			want:  7,
		},
		{
			name:  "unknown group is bounded by limit only",
			group: "stranger",
			limit: 3,
			want:  3,
		},
		{
			name: "a waiting higher priority group leaves only what is held",
			setup: func(a *Allocator) {
				a.Acquire("g", 1)
				a.Acquire("high", 2)
			},
			group: "g",
			limit: 10,
			want:  1,
		},
		{
			name:  "another group's unmet minimum is kept free",
			extra: []GroupSpec{spec("steady", 0, 1, res(4, 1))},
			group: "g",
			limit: 10,
			want:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			specs := append([]GroupSpec{
				spec("g", 0, 0, res(4, 1)),
				spec("high", 50, 0, res(8, 1)),
				spec("free-rider", 0, 0, Resources{}),
			}, tt.extra...)
			a := newAllocator(res(10, 100), specs...)
			if tt.setup != nil {
				tt.setup(a)
			}
			if got := a.Capacity(tt.group, tt.limit); got != tt.want {
				t.Fatalf("Capacity(%s, %d) = %d, want %d", tt.group, tt.limit, got, tt.want)
			}
		})
	}
}

func TestCapacity_SaturatedHostAdvertisesOnlyWarmRunners(t *testing.T) {
	a := newAllocator(res(8, 100),
		spec("warm", 0, 2, res(1, 1)),
		spec("hog", 50, 0, res(6, 1)),
	)
	mustAcquire(t, a, "warm", 2, 2, 0)
	mustAcquire(t, a, "hog", 1, 1, 0)

	if got := a.Capacity("warm", 10); got != 2 {
		t.Fatalf("Capacity(warm) = %d, want 2: warm runners must not attract jobs the host cannot add", got)
	}
}

func TestCapacity_UnmetMinimumIsAdmittedPastPriority(t *testing.T) {
	a := newAllocator(res(8, 100),
		spec("steady", 0, 1, res(2, 1)),
		spec("high", 50, 0, res(8, 1)),
	)
	mustAcquire(t, a, "high", 1, 0, 1)

	if got := a.Capacity("steady", 5); got != 1 {
		t.Fatalf("Capacity(steady) = %d, want 1 for its unmet minimum", got)
	}
}

func TestCapacity_ReservesNothing(t *testing.T) {
	a := newAllocator(res(10, 10), spec("g", 0, 0, res(4, 4)))
	a.Capacity("g", 5)

	if st := a.Status(); !st.Used.IsZero() || st.Groups["g"].Reserved != 0 {
		t.Fatalf("Capacity changed the budget: %+v", st)
	}
	mustAcquire(t, a, "g", 2, 2, 0)
}

type capacityWatcher struct {
	alloc *Allocator
	group string
	limit int
	last  atomic.Int64
}

func (w *capacityWatcher) Wake(context.Context) {}

func (w *capacityWatcher) CapacityChanged() {
	w.last.Store(int64(w.alloc.Capacity(w.group, w.limit)))
}

func TestRun_CapacityChangesReachEveryGroupUnderChurn(t *testing.T) {
	a := newAllocator(res(12, 100),
		spec("a", 0, 0, res(2, 1)),
		spec("b", 10, 1, res(3, 1)),
		spec("c", 5, 0, res(4, 1)),
	)
	watchers := map[string]*capacityWatcher{}
	for _, name := range []string{"a", "b", "c"} {
		w := &capacityWatcher{alloc: a, group: name, limit: 4}
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
		want := int64(a.Capacity(name, w.limit))
		for w.last.Load() != want {
			if time.Now().After(deadline) {
				t.Fatalf("group %s last saw capacity %d, current is %d", name, w.last.Load(), want)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}

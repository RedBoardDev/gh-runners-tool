package capacity

import (
	"context"
	"log/slog"
	"sync"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/logging"
)

type GroupSpec struct {
	Name       string
	Priority   int
	MinRunners int
	Reserve    Resources
}

type Waker interface {
	Wake(ctx context.Context)
}

type Grant struct {
	Leases  []*Lease
	Waiting int
}

type groupEntry struct {
	spec    GroupSpec
	held    int
	waiting int
	waitSeq uint64
	waker   Waker
	closed  bool
}

type Allocator struct {
	mu      sync.Mutex
	total   Resources
	used    Resources
	groups  map[string]*groupEntry
	nextSeq uint64
	wakeCh  chan struct{}
	logger  *slog.Logger
}

func New(total Resources, specs []GroupSpec, logger *slog.Logger) *Allocator {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	groups := make(map[string]*groupEntry, len(specs))
	for _, spec := range specs {
		groups[spec.Name] = &groupEntry{spec: spec}
	}
	return &Allocator{
		total:  total,
		groups: groups,
		wakeCh: make(chan struct{}, 1),
		logger: logger,
	}
}

func (a *Allocator) Register(group string, w Waker) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if e, ok := a.groups[group]; ok {
		e.waker = w
		e.closed = false
	}
}

func (a *Allocator) Unregister(group string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if e, ok := a.groups[group]; ok {
		e.waker = nil
		e.closed = true
		e.waiting = 0
	}
}

func (a *Allocator) Acquire(group string, want int) Grant {
	want = max(want, 0)

	a.mu.Lock()
	e, ok := a.groups[group]
	if !ok || e.spec.Reserve.IsZero() {
		grant := a.grantUnbounded(group, e, want)
		a.mu.Unlock()
		return grant
	}

	var leases []*Lease
	for len(leases) < want && a.admit(e) {
		a.used = addResources(a.used, e.spec.Reserve)
		e.held++
		leases = append(leases, &Lease{alloc: a, group: group, reserve: e.spec.Reserve})
	}
	waiting := want - len(leases)
	change := waitingUnchanged
	if !e.closed {
		change = a.updateWaiting(e, waiting)
	}
	freeNow := free(a.total, a.used)
	a.mu.Unlock()

	a.logWaitingChange(group, e.spec.Reserve, freeNow, waiting, change)
	return Grant{Leases: leases, Waiting: waiting}
}

func (a *Allocator) grantUnbounded(group string, e *groupEntry, want int) Grant {
	leases := make([]*Lease, 0, want)
	for range want {
		if e != nil {
			e.held++
		}
		leases = append(leases, &Lease{alloc: a, group: group})
	}
	return Grant{Leases: leases}
}

type waitingChange int

const (
	waitingUnchanged waitingChange = iota
	waitingStarted
	waitingStopped
)

func (a *Allocator) updateWaiting(e *groupEntry, waiting int) waitingChange {
	previous := e.waiting
	e.waiting = waiting
	switch {
	case waiting > 0 && previous == 0:
		a.nextSeq++
		e.waitSeq = a.nextSeq
		return waitingStarted
	case waiting == 0 && previous > 0:
		return waitingStopped
	default:
		return waitingUnchanged
	}
}

func (a *Allocator) logWaitingChange(group string, needed, freeNow Resources, waiting int, change waitingChange) {
	switch change {
	case waitingStarted:
		a.logger.Info("group waiting for capacity",
			logging.KeyGroup, group,
			"waiting_runners", waiting,
			"needed_per_runner", needed.String(),
			"free", freeNow.String(),
		)
	case waitingStopped:
		a.logger.Info("group no longer waiting for capacity",
			logging.KeyGroup, group,
			"free", freeNow.String(),
		)
	case waitingUnchanged:
	}
}

func (a *Allocator) WaitingRunners() map[string]int {
	a.mu.Lock()
	defer a.mu.Unlock()

	out := make(map[string]int)
	for name, e := range a.groups {
		if e.waiting > 0 {
			out[name] = e.waiting
		}
	}
	return out
}

func (a *Allocator) signalWake() {
	select {
	case a.wakeCh <- struct{}{}:
	default:
	}
}

func (a *Allocator) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-a.wakeCh:
			a.wakeWaiting(ctx)
		}
	}
}

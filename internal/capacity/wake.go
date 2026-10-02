package capacity

import (
	"cmp"
	"context"
	"slices"
)

func (a *Allocator) wakeWaiting(ctx context.Context) {
	for _, w := range a.waitingWakers() {
		if ctx.Err() != nil {
			return
		}
		w.Wake(ctx)
	}
}

func (a *Allocator) waitingWakers() []Waker {
	a.mu.Lock()
	defer a.mu.Unlock()

	waiting := make([]*groupEntry, 0, len(a.groups))
	for _, e := range a.groups {
		if e.waiting > 0 && e.waker != nil {
			waiting = append(waiting, e)
		}
	}
	slices.SortFunc(waiting, func(x, y *groupEntry) int {
		if c := cmp.Compare(y.spec.Priority, x.spec.Priority); c != 0 {
			return c
		}
		return cmp.Compare(x.waitSeq, y.waitSeq)
	})

	wakers := make([]Waker, len(waiting))
	for i, e := range waiting {
		wakers[i] = e.waker
	}
	return wakers
}

package capacity

import (
	"cmp"
	"slices"
)

func (a *Allocator) planAdvertised() plan {
	groups := a.advertisingGroups()
	planned := make(plan, len(groups))
	used := a.used
	take := func(e *groupEntry) bool {
		if e.held+planned[e] >= e.spec.MaxRunners {
			return false
		}
		if !a.admit(e, planned, used) {
			return false
		}
		planned[e]++
		used = addResources(used, e.spec.Reserve)
		return true
	}

	contended := slices.ContainsFunc(groups, func(e *groupEntry) bool { return e.waiting > 0 })
	for _, e := range groups {
		for pending(e, planned) > 0 {
			if !take(e) {
				break
			}
		}
	}

	for dealt := true; dealt; {
		dealt = false
		for _, e := range groups {
			dealt = take(e) || dealt
		}
	}
	if contended {
		return planned
	}

	// Without this, an idle group ranked first keeps the last free runner
	// advertised forever and a lower group's jobs never reach this host.
	for _, e := range groups {
		if planned[e] == 0 && e.held < e.spec.MaxRunners && a.admit(e, nil, a.used) {
			planned[e] = 1
		}
	}
	return planned
}

func (a *Allocator) advertisingGroups() []*groupEntry {
	groups := make([]*groupEntry, 0, len(a.groups))
	for _, e := range a.groups {
		if !e.closed && !e.spec.Reserve.IsZero() {
			groups = append(groups, e)
		}
	}
	slices.SortFunc(groups, func(x, y *groupEntry) int {
		if c := cmp.Compare(y.spec.Priority, x.spec.Priority); c != 0 {
			return c
		}
		if (x.waiting > 0) != (y.waiting > 0) {
			if x.waiting > 0 {
				return -1
			}
			return 1
		}
		if x.waiting > 0 {
			return cmp.Compare(x.waitSeq, y.waitSeq)
		}
		return cmp.Compare(x.spec.Name, y.spec.Name)
	})
	return groups
}

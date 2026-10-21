package capacity

type plan map[*groupEntry]int

func (a *Allocator) admit(e *groupEntry, planned plan, used Resources) bool {
	need := e.spec.Reserve
	if !fits(a.total, used, need) {
		return false
	}
	if e.held+planned[e] < e.spec.MinRunners {
		return true
	}
	if !fits(a.total, used, addResources(need, a.unmetFloors(e, planned))) {
		return false
	}
	return !a.blockedByPriority(e, planned)
}

func (a *Allocator) unmetFloors(except *groupEntry, planned plan) Resources {
	var sum Resources
	for _, h := range a.groups {
		if h == except {
			continue
		}
		if missing := h.spec.MinRunners - h.held - planned[h]; missing > 0 {
			sum = addResources(sum, scaleResources(h.spec.Reserve, missing))
		}
	}
	return sum
}

func (a *Allocator) blockedByPriority(e *groupEntry, planned plan) bool {
	for _, h := range a.groups {
		if h == e || pending(h, planned) == 0 || h.spec.Reserve.IsZero() {
			continue
		}
		if precedes(h, e, planned) {
			return true
		}
	}
	return false
}

func precedes(h, e *groupEntry, planned plan) bool {
	if h.spec.Priority != e.spec.Priority {
		return h.spec.Priority > e.spec.Priority
	}
	if pending(e, planned) == 0 {
		return true
	}
	return h.waitSeq < e.waitSeq
}

func pending(e *groupEntry, planned plan) int {
	return max(e.waiting-planned[e], 0)
}

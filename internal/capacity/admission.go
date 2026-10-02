package capacity

func (a *Allocator) admit(e *groupEntry) bool {
	need := e.spec.Reserve
	if !fits(a.total, a.used, need) {
		return false
	}
	if e.held < e.spec.MinRunners {
		return true
	}
	if !fits(a.total, a.used, addResources(need, a.unmetFloors(e))) {
		return false
	}
	return !a.blockedByPriority(e)
}

func (a *Allocator) unmetFloors(except *groupEntry) Resources {
	var sum Resources
	for _, h := range a.groups {
		if h == except {
			continue
		}
		if missing := h.spec.MinRunners - h.held; missing > 0 {
			sum = addResources(sum, scaleResources(h.spec.Reserve, missing))
		}
	}
	return sum
}

func (a *Allocator) blockedByPriority(e *groupEntry) bool {
	for _, h := range a.groups {
		if h == e || h.waiting == 0 || h.spec.Reserve.IsZero() {
			continue
		}
		if precedes(h, e) {
			return true
		}
	}
	return false
}

func precedes(h, e *groupEntry) bool {
	if h.spec.Priority != e.spec.Priority {
		return h.spec.Priority > e.spec.Priority
	}
	if e.waiting == 0 {
		return true
	}
	return h.waitSeq < e.waitSeq
}

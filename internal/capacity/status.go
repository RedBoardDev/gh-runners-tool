package capacity

type GroupStatus struct {
	Priority int       `json:"priority"`
	Reserve  Resources `json:"reserve"`
	Reserved int       `json:"reserved"`
	Waiting  int       `json:"waiting"`
}

type Status struct {
	Total  Resources              `json:"total"`
	Used   Resources              `json:"used"`
	Free   Resources              `json:"free"`
	Groups map[string]GroupStatus `json:"groups"`
}

func (a *Allocator) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()

	groups := make(map[string]GroupStatus, len(a.groups))
	for name, e := range a.groups {
		groups[name] = GroupStatus{
			Priority: e.spec.Priority,
			Reserve:  e.spec.Reserve,
			Reserved: e.held,
			Waiting:  e.waiting,
		}
	}
	return Status{
		Total:  a.total,
		Used:   a.used,
		Free:   free(a.total, a.used),
		Groups: groups,
	}
}

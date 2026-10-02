package capacity

type Lease struct {
	alloc   *Allocator
	group   string
	reserve Resources
	done    bool
}

func (l *Lease) Release() {
	if l.finish() {
		l.alloc.signalWake()
	}
}

// Waking waiters from Cancel makes a group whose runners fail to start retry in a hot loop.
func (l *Lease) Cancel() {
	l.finish()
}

func (l *Lease) finish() bool {
	if l == nil {
		return false
	}
	a := l.alloc
	a.mu.Lock()
	defer a.mu.Unlock()
	if l.done {
		return false
	}
	l.done = true
	a.used = subResources(a.used, l.reserve)
	if e, ok := a.groups[l.group]; ok {
		e.held--
	}
	return true
}

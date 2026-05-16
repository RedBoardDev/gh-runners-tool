package state

import (
	"path/filepath"
	"testing"
)

func TestPaths(t *testing.T) {
	p := New("/var/lib/ghr/state")

	if got, want := p.PIDFile(), filepath.Join("/var/lib/ghr/state", "daemon.pid"); got != want {
		t.Errorf("PIDFile() = %q, want %q", got, want)
	}
	if got, want := p.StateFile(), filepath.Join("/var/lib/ghr/state", "daemon.state.json"); got != want {
		t.Errorf("StateFile() = %q, want %q", got, want)
	}
	if got, want := p.Socket(), filepath.Join("/var/lib/ghr/state", "ghr.sock"); got != want {
		t.Errorf("Socket() = %q, want %q", got, want)
	}

	all := p.All()
	if len(all) != 3 {
		t.Fatalf("All() returned %d paths, want 3", len(all))
	}
}

package state

import "path/filepath"

const (
	pidFileName    = "daemon.pid"
	stateFileName  = "daemon.state.json"
	socketFileName = "ghr.sock"
)

type Paths struct {
	Dir string
}

func New(dir string) Paths {
	return Paths{Dir: dir}
}

func (p Paths) PIDFile() string {
	return filepath.Join(p.Dir, pidFileName)
}

func (p Paths) StateFile() string {
	return filepath.Join(p.Dir, stateFileName)
}

func (p Paths) Socket() string {
	return filepath.Join(p.Dir, socketFileName)
}

func (p Paths) All() []string {
	return []string{p.PIDFile(), p.StateFile(), p.Socket()}
}

package domain

type Group struct {
	Name      string
	Count     int
	Ephemeral bool
	Labels    []string
	Workdir   string
	Version   string
}

type RunnerInstance struct {
	ID        string
	GroupName string
	Ephemeral bool
	Workdir   string
	Labels    []string
	Version   string
}

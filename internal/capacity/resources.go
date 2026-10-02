package capacity

import "fmt"

type Resources struct {
	MemoryBytes int64 `json:"memory_bytes"`
	CPUMilli    int64 `json:"cpu_milli"`
}

func (r Resources) IsZero() bool {
	return r.MemoryBytes == 0 && r.CPUMilli == 0
}

func (r Resources) String() string {
	return fmt.Sprintf("%.1f GiB, %.2f cpus", float64(r.MemoryBytes)/(1<<30), float64(r.CPUMilli)/1000)
}

func addResources(a, b Resources) Resources {
	return Resources{MemoryBytes: a.MemoryBytes + b.MemoryBytes, CPUMilli: a.CPUMilli + b.CPUMilli}
}

func subResources(a, b Resources) Resources {
	return Resources{MemoryBytes: a.MemoryBytes - b.MemoryBytes, CPUMilli: a.CPUMilli - b.CPUMilli}
}

func scaleResources(r Resources, n int) Resources {
	return Resources{MemoryBytes: r.MemoryBytes * int64(n), CPUMilli: r.CPUMilli * int64(n)}
}

func fits(total, used, need Resources) bool {
	if total.MemoryBytes > 0 && used.MemoryBytes+need.MemoryBytes > total.MemoryBytes {
		return false
	}
	if total.CPUMilli > 0 && used.CPUMilli+need.CPUMilli > total.CPUMilli {
		return false
	}
	return true
}

func free(total, used Resources) Resources {
	var out Resources
	if total.MemoryBytes > 0 {
		out.MemoryBytes = total.MemoryBytes - used.MemoryBytes
	}
	if total.CPUMilli > 0 {
		out.CPUMilli = total.CPUMilli - used.CPUMilli
	}
	return out
}

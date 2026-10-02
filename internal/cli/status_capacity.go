package cli

import (
	"fmt"
	"sort"
)

type statusResources struct {
	MemoryBytes int64 `json:"memory_bytes"`
	CPUMilli    int64 `json:"cpu_milli"`
}

type statusCapacityGroup struct {
	Priority int             `json:"priority"`
	Reserve  statusResources `json:"reserve"`
	Reserved int             `json:"reserved"`
	Waiting  int             `json:"waiting"`
}

type statusCapacity struct {
	Total  statusResources                `json:"total"`
	Used   statusResources                `json:"used"`
	Free   statusResources                `json:"free"`
	Groups map[string]statusCapacityGroup `json:"groups"`
}

func renderCapacitySection(c *statusCapacity) {
	if c == nil {
		return
	}

	fmt.Println("Capacity")
	fmt.Printf("  Memory:  %s\n", describeUsage(formatGiB(c.Used.MemoryBytes), formatGiB(c.Total.MemoryBytes), formatGiB(c.Free.MemoryBytes), c.Total.MemoryBytes))
	fmt.Printf("  CPUs:    %s\n", describeUsage(formatCPUs(c.Used.CPUMilli), formatCPUs(c.Total.CPUMilli), formatCPUs(c.Free.CPUMilli), c.Total.CPUMilli))
	fmt.Println()

	fmt.Printf("  %-20s %8s %-18s %8s %7s\n", "Group", "Priority", "Reserve", "Reserved", "Waiting")
	fmt.Printf("  %-20s %8s %-18s %8s %7s\n", "-----", "--------", "-------", "--------", "-------")

	names := make([]string, 0, len(c.Groups))
	for name := range c.Groups {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		g := c.Groups[name]
		reserve := fmt.Sprintf("%s / %s", formatGiB(g.Reserve.MemoryBytes), formatCPUs(g.Reserve.CPUMilli))
		fmt.Printf("  %-20s %8d %-18s %8d %7d\n", name, g.Priority, reserve, g.Reserved, g.Waiting)
	}
	fmt.Println()
}

func describeUsage(used, total, free string, rawTotal int64) string {
	if rawTotal == 0 {
		return fmt.Sprintf("%s used (unlimited)", used)
	}
	return fmt.Sprintf("%s used / %s (%s free)", used, total, free)
}

func formatGiB(bytes int64) string {
	return fmt.Sprintf("%.1f GiB", float64(bytes)/(1<<30))
}

func formatCPUs(milli int64) string {
	return fmt.Sprintf("%.1f", float64(milli)/1000)
}

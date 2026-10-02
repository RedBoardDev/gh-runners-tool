package cli

import (
	"encoding/json"
	"testing"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/capacity"
)

func TestStatusCapacity_DecodesTheAPIPayload(t *testing.T) {
	api := capacity.Status{
		Total: capacity.Resources{MemoryBytes: 48 << 30, CPUMilli: 40_000},
		Used:  capacity.Resources{MemoryBytes: 18 << 30, CPUMilli: 12_000},
		Free:  capacity.Resources{MemoryBytes: 30 << 30, CPUMilli: 28_000},
		Groups: map[string]capacity.GroupStatus{
			"heavy": {Priority: 50, Reserve: capacity.Resources{MemoryBytes: 9 << 30, CPUMilli: 6000}, Reserved: 2, Waiting: 1},
		},
	}
	payload, err := json.Marshal(map[string]any{"groups": map[string]any{}, "capacity": api})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var status statusResponse
	if err := json.Unmarshal(payload, &status); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	c := status.Capacity
	if c == nil {
		t.Fatal("capacity not decoded")
	}
	if c.Total.MemoryBytes != 48<<30 || c.Used.CPUMilli != 12_000 || c.Free.MemoryBytes != 30<<30 {
		t.Errorf("totals not decoded: %+v", c)
	}
	g := c.Groups["heavy"]
	if g.Priority != 50 || g.Reserved != 2 || g.Waiting != 1 || g.Reserve.CPUMilli != 6000 {
		t.Errorf("group not decoded: %+v", g)
	}
}

func TestStatusCapacity_AbsentWhenDaemonHasNoBudget(t *testing.T) {
	var status statusResponse
	if err := json.Unmarshal([]byte(`{"groups":{},"health":{}}`), &status); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if status.Capacity != nil {
		t.Fatalf("capacity = %+v, want nil", status.Capacity)
	}
}

func TestDescribeUsage(t *testing.T) {
	tests := []struct {
		name     string
		rawTotal int64
		want     string
	}{
		{"limited", 100, "1.0 GiB used / 4.0 GiB (3.0 GiB free)"},
		{"unlimited dimension", 0, "1.0 GiB used (unlimited)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := describeUsage(formatGiB(1<<30), formatGiB(4<<30), formatGiB(3<<30), tt.rawTotal)
			if got != tt.want {
				t.Fatalf("describeUsage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatCPUs(t *testing.T) {
	tests := []struct {
		milli int64
		want  string
	}{
		{0, "0.0"},
		{500, "0.5"},
		{40_000, "40.0"},
	}
	for _, tt := range tests {
		if got := formatCPUs(tt.milli); got != tt.want {
			t.Errorf("formatCPUs(%d) = %q, want %q", tt.milli, got, tt.want)
		}
	}
}

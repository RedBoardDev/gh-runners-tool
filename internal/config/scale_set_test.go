package config

import (
	"strings"
	"testing"
)

func TestScaleSetName(t *testing.T) {
	tests := []struct {
		name   string
		group  string
		hostID string
		want   string
	}{
		{"without host_id the group name is kept", "kare-api", "", "kare-api"},
		{"host_id is appended to the group name", "kare-api", "hel1", "kare-api-hel1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ScaleSetName(tt.group, tt.hostID); got != tt.want {
				t.Fatalf("ScaleSetName(%q, %q) = %q, want %q", tt.group, tt.hostID, got, tt.want)
			}
		})
	}
}

func TestLoad_HostID(t *testing.T) {
	tests := []struct {
		name      string
		hostID    string
		group     string
		wantInErr string
	}{
		{name: "valid host_id", hostID: "hel1.ex44", group: "kare-api"},
		{name: "unset host_id keeps any group name", hostID: "", group: strings.Repeat("g", 70)},
		{name: "space is rejected", hostID: "hel 1", group: "kare-api", wantInErr: `host_id "hel 1" must match`},
		{name: "leading dash is rejected", hostID: "-hel1", group: "kare-api", wantInErr: `host_id "-hel1" must match`},
		{name: "dash is rejected so names cannot collide across hosts", hostID: "x-y", group: "ci", wantInErr: `host_id "x-y" must match`},
		{
			name:      "scale set name over 64 characters is rejected",
			hostID:    strings.Repeat("h", 30),
			group:     strings.Repeat("g", 34),
			wantInErr: "groups[0] (" + strings.Repeat("g", 34) + "): scale set name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yaml := "host_id: \"" + tt.hostID + "\"\ngroups:\n  - name: " + tt.group + "\n    max_runners: 1\n"
			cfg, err := Load(writeConfig(t, yaml))
			if tt.wantInErr == "" {
				if err != nil {
					t.Fatalf("Load() unexpected error: %v", err)
				}
				if cfg.HostID != tt.hostID {
					t.Fatalf("host_id = %q, want %q", cfg.HostID, tt.hostID)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantInErr) {
				t.Fatalf("Load() error = %v, want substring %q", err, tt.wantInErr)
			}
		})
	}
}

func TestLoad_ScaleSetNameCollision(t *testing.T) {
	tests := []struct {
		name      string
		hostID    string
		wantInErr string
	}{
		{name: "without host_id", hostID: "", wantInErr: `groups[1] (CI): scale set name "CI" collides with groups[0] (ci)`},
		{name: "with host_id", hostID: "hel1", wantInErr: `groups[1] (CI): scale set name "CI-hel1" collides with groups[0] (ci)`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yaml := "host_id: \"" + tt.hostID + "\"\ngroups:\n  - name: ci\n    max_runners: 1\n  - name: CI\n    max_runners: 1\n"
			_, err := Load(writeConfig(t, yaml))
			if err == nil || !strings.Contains(err.Error(), tt.wantInErr) {
				t.Fatalf("Load() error = %v, want substring %q", err, tt.wantInErr)
			}
		})
	}
}

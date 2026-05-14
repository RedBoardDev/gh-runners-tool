package notification

import "testing"

func TestEventFilter_Matches(t *testing.T) {
	tests := []struct {
		name      string
		patterns  []string
		eventType string
		level     string
		want      bool
	}{
		{
			name:      "empty patterns matches everything",
			patterns:  nil,
			eventType: "daemon.start",
			level:     "info",
			want:      true,
		},
		{
			name:      "exact match",
			patterns:  []string{"daemon.start"},
			eventType: "daemon.start",
			level:     "info",
			want:      true,
		},
		{
			name:      "exact match no match",
			patterns:  []string{"daemon.stop"},
			eventType: "daemon.start",
			level:     "info",
			want:      false,
		},
		{
			name:      "wildcard matches prefix",
			patterns:  []string{"health.*"},
			eventType: "health.zombie_runner",
			level:     "error",
			want:      true,
		},
		{
			name:      "wildcard does not match different prefix",
			patterns:  []string{"health.*"},
			eventType: "daemon.start",
			level:     "info",
			want:      false,
		},
		{
			name:      "wildcard does not match partial prefix",
			patterns:  []string{"health.*"},
			eventType: "healthcheck.run",
			level:     "info",
			want:      false,
		},
		{
			name:      "level filter matches",
			patterns:  []string{"*:error"},
			eventType: "health.zombie_runner",
			level:     "error",
			want:      true,
		},
		{
			name:      "level filter does not match different level",
			patterns:  []string{"*:error"},
			eventType: "daemon.start",
			level:     "info",
			want:      false,
		},
		{
			name:      "level filter case insensitive",
			patterns:  []string{"*:Error"},
			eventType: "anything",
			level:     "error",
			want:      true,
		},
		{
			name:      "multiple patterns any match succeeds",
			patterns:  []string{"daemon.start", "health.*", "*:critical"},
			eventType: "health.disk_low",
			level:     "warning",
			want:      true,
		},
		{
			name:      "multiple patterns none match",
			patterns:  []string{"daemon.start", "runner.failed"},
			eventType: "health.zombie_runner",
			level:     "error",
			want:      false,
		},
		{
			name:      "empty patterns list explicit",
			patterns:  []string{},
			eventType: "daemon.start",
			level:     "info",
			want:      true,
		},
		{
			name:      "wildcard matches exact prefix dot event",
			patterns:  []string{"runner.*"},
			eventType: "runner.started",
			level:     "info",
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := EventFilter{Patterns: tt.patterns}
			got := f.Matches(tt.eventType, tt.level)
			if got != tt.want {
				t.Errorf("Matches(%q, %q) = %v, want %v", tt.eventType, tt.level, got, tt.want)
			}
		})
	}
}

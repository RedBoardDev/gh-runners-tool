package controller

import (
	"testing"
	"time"
)

func TestNextBackoff_JitteredWithinBounds(t *testing.T) {
	const samples = 200
	current := 4 * time.Second

	for i := 0; i < samples; i++ {
		got := nextBackoff(current)
		// next would be current * 2 = 8s.
		low := time.Duration(float64(8*time.Second) * 0.8)
		high := time.Duration(float64(8*time.Second) * 1.2)
		if got < low || got > high {
			t.Fatalf("nextBackoff(%s) = %s, want within [%s, %s]", current, got, low, high)
		}
	}
}

func TestNextBackoff_RespectsCap(t *testing.T) {
	current := backoffMax
	for i := 0; i < 100; i++ {
		got := nextBackoff(current)
		// Capped at backoffMax (with ±20% jitter on the cap itself).
		if got < time.Duration(float64(backoffMax)*0.8) || got > time.Duration(float64(backoffMax)*1.2) {
			t.Fatalf("nextBackoff(cap) = %s, expected within ±20%% of %s", got, backoffMax)
		}
	}
}

func TestNextBackoff_VariesAcrossCalls(t *testing.T) {
	current := 4 * time.Second
	seen := make(map[time.Duration]struct{})
	for i := 0; i < 20; i++ {
		seen[nextBackoff(current)] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatalf("expected jittered values across calls, got %d unique results", len(seen))
	}
}

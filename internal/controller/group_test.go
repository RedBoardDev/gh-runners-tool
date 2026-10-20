package controller

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/actions/scaleset"
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

type registryClient struct {
	*fakeScaleSetClient
	registered map[string]scaleset.RunnerReference
	lookupErr  map[string]error
}

func (r *registryClient) GetRunnerByName(_ context.Context, name string) (*scaleset.RunnerReference, error) {
	if err := r.lookupErr[name]; err != nil {
		return nil, err
	}
	ref, ok := r.registered[name]
	if !ok {
		return nil, nil
	}
	return &ref, nil
}

func TestRemoveStaleRegistrations_RemovesOnlyThisHostsLeftoversInThisScaleSet(t *testing.T) {
	const scaleSetID = 5
	client := &registryClient{
		fakeScaleSetClient: &fakeScaleSetClient{},
		registered: map[string]scaleset.RunnerReference{
			"g-left":        {ID: 11, Name: "g-left", RunnerScaleSetID: scaleSetID},
			"g-other-set":   {ID: 12, Name: "g-other-set", RunnerScaleSetID: 9},
			"g-other-host":  {ID: 13, Name: "g-other-host", RunnerScaleSetID: scaleSetID},
			"other-left":    {ID: 14, Name: "other-left", RunnerScaleSetID: 6},
			"g-lookup-fail": {ID: 15, Name: "g-lookup-fail", RunnerScaleSetID: scaleSetID},
		},
		lookupErr: map[string]error{"g-lookup-fail": errors.New("timeout")},
	}
	c := New(client, nil, nil, nil, nil, nil, ControllerConfig{StaleRunners: map[string][]string{
		"g":     {"g-left", "g-other-set", "g-gone", "g-lookup-fail"},
		"other": {"other-left"},
	}}, testLogger())

	c.removeStaleRegistrations(context.Background(), "g", scaleSetID, testLogger())
	c.removeStaleRegistrations(context.Background(), "g", scaleSetID, testLogger())

	if want := []int{11}; !slices.Equal(client.removed, want) {
		t.Fatalf("removed runner IDs = %v, want %v: only this host's leftovers in this scale set, never another host's live runner", client.removed, want)
	}
}

type flakyRemoveClient struct {
	*registryClient
	failures int
}

func (f *flakyRemoveClient) RemoveRunner(ctx context.Context, id int) error {
	if f.failures > 0 {
		f.failures--
		return errors.New("service unavailable")
	}
	return f.registryClient.RemoveRunner(ctx, id)
}

func TestRemoveStaleRegistrations_RetriesTransientFailuresOnTheNextListenerStart(t *testing.T) {
	const scaleSetID = 5
	registry := &registryClient{
		fakeScaleSetClient: &fakeScaleSetClient{},
		registered: map[string]scaleset.RunnerReference{
			"g-lookup": {ID: 11, Name: "g-lookup", RunnerScaleSetID: scaleSetID},
			"g-remove": {ID: 12, Name: "g-remove", RunnerScaleSetID: scaleSetID},
		},
		lookupErr: map[string]error{"g-lookup": errors.New("timeout")},
	}
	client := &flakyRemoveClient{registryClient: registry, failures: 1}
	c := New(client, nil, nil, nil, nil, nil, ControllerConfig{StaleRunners: map[string][]string{
		"g": {"g-lookup", "g-remove", "g-gone"},
	}}, testLogger())

	c.removeStaleRegistrations(context.Background(), "g", scaleSetID, testLogger())
	if len(registry.removed) != 0 {
		t.Fatalf("removed runner IDs = %v after only transient failures, want none", registry.removed)
	}
	delete(registry.lookupErr, "g-lookup")
	c.removeStaleRegistrations(context.Background(), "g", scaleSetID, testLogger())
	c.removeStaleRegistrations(context.Background(), "g", scaleSetID, testLogger())

	got := slices.Sorted(slices.Values(registry.removed))
	if want := []int{11, 12}; !slices.Equal(got, want) {
		t.Fatalf("removed runner IDs = %v, want %v: each leftover retried until it is removed, then never again", got, want)
	}
}

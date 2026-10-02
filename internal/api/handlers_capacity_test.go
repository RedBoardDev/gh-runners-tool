package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/capacity"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/health"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
)

type mockCapacity struct {
	status capacity.Status
}

func (m *mockCapacity) Status() capacity.Status {
	return m.status
}

func getStatus(t *testing.T, s *Server) map[string]json.RawMessage {
	t.Helper()
	srv := httptest.NewServer(s.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/status")
	if err != nil {
		t.Fatalf("GET /status: %v", err)
	}
	defer resp.Body.Close()

	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body
}

func TestHandleStatus_ExposesCapacity(t *testing.T) {
	s := testServer(&mockController{snapshots: map[string][]model.RunnerSnapshot{}}, &mockHealth{status: health.HealthStatus{}})
	WithCapacity(&mockCapacity{status: capacity.Status{
		Total: capacity.Resources{MemoryBytes: 48 << 30, CPUMilli: 40_000},
		Used:  capacity.Resources{MemoryBytes: 18 << 30, CPUMilli: 12_000},
		Free:  capacity.Resources{MemoryBytes: 30 << 30, CPUMilli: 28_000},
		Groups: map[string]capacity.GroupStatus{
			"heavy": {Priority: 50, Reserve: capacity.Resources{MemoryBytes: 9 << 30, CPUMilli: 6000}, Reserved: 2, Waiting: 1},
		},
	}})(s)

	body := getStatus(t, s)

	raw, ok := body["capacity"]
	if !ok {
		t.Fatalf("response lacks capacity: %v", body)
	}
	var got capacity.Status
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode capacity: %v", err)
	}
	if got.Total.MemoryBytes != 48<<30 || got.Used.CPUMilli != 12_000 || got.Free.MemoryBytes != 30<<30 {
		t.Errorf("totals/used/free lost in transit: %+v", got)
	}
	if g := got.Groups["heavy"]; g.Reserved != 2 || g.Waiting != 1 || g.Priority != 50 {
		t.Errorf("group counts lost in transit: %+v", g)
	}
}

func TestHandleStatus_OmitsCapacityWhenNotConfigured(t *testing.T) {
	s := testServer(&mockController{snapshots: map[string][]model.RunnerSnapshot{}}, &mockHealth{status: health.HealthStatus{}})

	body := getStatus(t, s)

	if _, ok := body["capacity"]; ok {
		t.Fatal("capacity must be absent when no budget is configured")
	}
}

package controller

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/logging"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/runner"
)

type fakeScaleSetClient struct {
	scaleSetClient
	jitID     int
	jitErr    error
	removed   []int
	removeErr error
}

func (f *fakeScaleSetClient) GenerateJITConfig(_ context.Context, _ int, _ string) (string, int, error) {
	if f.jitErr != nil {
		return "", 0, f.jitErr
	}
	return "encoded-jit", f.jitID, nil
}

func (f *fakeScaleSetClient) RemoveRunner(_ context.Context, id int) error {
	f.removed = append(f.removed, id)
	return f.removeErr
}

type fakeProcess struct {
	prepareErr error
	startErr   error
	startProc  *runner.Process
}

func (f *fakeProcess) Prepare(_ context.Context, _ *model.RunnerInstance, _ string) (string, error) {
	if f.prepareErr != nil {
		return "", f.prepareErr
	}
	return "/tmp/workdir", nil
}

func (f *fakeProcess) Start(_ context.Context, _ *model.RunnerInstance, _, _ string, _ io.Writer) (*runner.Process, error) {
	if f.startErr != nil {
		return nil, f.startErr
	}
	return f.startProc, nil
}

func (f *fakeProcess) Stop(_ context.Context, _ *runner.Process) error { return nil }

func (f *fakeProcess) Cleanup(_ *runner.Process) error { return nil }

func tempLogMgr(t *testing.T) *logging.LogManager {
	t.Helper()
	lm, err := logging.New(logging.LogConfig{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}
	return lm
}

func TestStartRunner_DeregistersOnPrepareFailure(t *testing.T) {
	client := &fakeScaleSetClient{jitID: 42}
	s := &MacOSScaler{
		client:    client,
		process:   &fakeProcess{prepareErr: errors.New("no space left on device")},
		groupName: "kare-qa",
		logger:    testLogger(),
		idle:      make(map[string]*runner.Process),
		busy:      make(map[string]*runner.Process),
	}

	if err := s.startRunner(context.Background()); err == nil {
		t.Fatal("expected error when prepare fails")
	}

	if len(client.removed) != 1 || client.removed[0] != 42 {
		t.Fatalf("expected runner 42 deregistered, got %v", client.removed)
	}
	if len(s.idle) != 0 {
		t.Fatalf("expected no idle runner tracked, got %d", len(s.idle))
	}
}

func TestStartRunner_DeregistersOnStartFailure(t *testing.T) {
	client := &fakeScaleSetClient{jitID: 7}
	s := &MacOSScaler{
		client:    client,
		process:   &fakeProcess{startErr: errors.New("exec failed")},
		logMgr:    tempLogMgr(t),
		groupName: "kare-qa",
		logger:    testLogger(),
		idle:      make(map[string]*runner.Process),
		busy:      make(map[string]*runner.Process),
	}

	if err := s.startRunner(context.Background()); err == nil {
		t.Fatal("expected error when start fails")
	}

	if len(client.removed) != 1 || client.removed[0] != 7 {
		t.Fatalf("expected runner 7 deregistered, got %v", client.removed)
	}
}

func TestStartRunner_NoDeregisterOnSuccess(t *testing.T) {
	client := &fakeScaleSetClient{jitID: 55}
	proc := &runner.Process{Name: "placeholder"}
	s := &MacOSScaler{
		client:    client,
		process:   &fakeProcess{startProc: proc},
		logMgr:    tempLogMgr(t),
		groupName: "kare-qa",
		logger:    testLogger(),
		idle:      make(map[string]*runner.Process),
		busy:      make(map[string]*runner.Process),
	}

	if err := s.startRunner(context.Background()); err != nil {
		t.Fatalf("startRunner: %v", err)
	}

	if len(client.removed) != 0 {
		t.Fatalf("expected no deregistration on success, got %v", client.removed)
	}
	if proc.RunnerID != 55 {
		t.Fatalf("expected RunnerID 55 set on process, got %d", proc.RunnerID)
	}
	if len(s.idle) != 1 {
		t.Fatalf("expected 1 idle runner tracked, got %d", len(s.idle))
	}
}

func TestKillRunner_DeregistersRunner(t *testing.T) {
	client := &fakeScaleSetClient{}
	s := &MacOSScaler{
		client:    client,
		process:   &fakeProcess{},
		logMgr:    tempLogMgr(t),
		groupName: "kare-qa",
		logger:    testLogger(),
		idle:      map[string]*runner.Process{"kare-qa-1": {Name: "kare-qa-1", RunnerID: 99}},
		busy:      make(map[string]*runner.Process),
	}

	if err := s.killRunner(context.Background(), "kare-qa-1"); err != nil {
		t.Fatalf("killRunner: %v", err)
	}

	if len(client.removed) != 1 || client.removed[0] != 99 {
		t.Fatalf("expected runner 99 deregistered, got %v", client.removed)
	}
}

func TestKillIdleRunner_KillsIdle(t *testing.T) {
	client := &fakeScaleSetClient{}
	s := &MacOSScaler{
		client:    client,
		process:   &fakeProcess{},
		logMgr:    tempLogMgr(t),
		groupName: "kare-qa",
		logger:    testLogger(),
		idle:      map[string]*runner.Process{"kare-qa-1": {Name: "kare-qa-1", RunnerID: 33}},
		busy:      make(map[string]*runner.Process),
	}

	if err := s.killIdleRunner(context.Background(), "kare-qa-1"); err != nil {
		t.Fatalf("killIdleRunner: %v", err)
	}

	if len(client.removed) != 1 || client.removed[0] != 33 {
		t.Fatalf("expected runner 33 deregistered, got %v", client.removed)
	}
	if len(s.idle) != 0 {
		t.Fatalf("expected runner removed from idle, got %d", len(s.idle))
	}
}

func TestKillIdleRunner_SkipsWhenBusy(t *testing.T) {
	client := &fakeScaleSetClient{}
	s := &MacOSScaler{
		client:    client,
		process:   &fakeProcess{},
		logMgr:    tempLogMgr(t),
		groupName: "kare-qa",
		logger:    testLogger(),
		idle:      make(map[string]*runner.Process),
		busy:      map[string]*runner.Process{"kare-qa-1": {Name: "kare-qa-1", RunnerID: 33}},
	}

	if err := s.killIdleRunner(context.Background(), "kare-qa-1"); err != nil {
		t.Fatalf("expected nil error when runner became busy, got %v", err)
	}

	if len(client.removed) != 0 {
		t.Fatalf("expected no deregistration, got %v", client.removed)
	}
	if _, ok := s.busy["kare-qa-1"]; !ok {
		t.Fatal("expected busy runner to be left running")
	}
}

func TestKillIdleRunner_NotFound(t *testing.T) {
	s := &MacOSScaler{
		groupName: "kare-qa",
		logger:    testLogger(),
		idle:      make(map[string]*runner.Process),
		busy:      make(map[string]*runner.Process),
	}

	if err := s.killIdleRunner(context.Background(), "kare-qa-missing"); err == nil {
		t.Fatal("expected error for missing runner")
	}
}

func TestDeregisterRunner_SkipsZeroID(t *testing.T) {
	client := &fakeScaleSetClient{}
	s := &MacOSScaler{
		client:    client,
		groupName: "kare-qa",
		logger:    testLogger(),
	}

	s.deregisterRunner(context.Background(), "kare-qa-1", 0)

	if len(client.removed) != 0 {
		t.Fatalf("expected no deregistration for zero ID, got %v", client.removed)
	}
}

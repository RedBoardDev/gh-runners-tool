package cli

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestSafeActor_CapturesPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	actor := safeActor(logger, "test", func() error {
		panic("boom")
	})

	err := actor()
	if err == nil {
		t.Fatal("expected error from panicking actor")
	}
	if !strings.Contains(err.Error(), "actor test panicked") {
		t.Errorf("error = %v, want substring 'actor test panicked'", err)
	}
	if !strings.Contains(buf.String(), "actor panicked") {
		t.Errorf("expected log entry 'actor panicked', got: %s", buf.String())
	}
}

func TestSafeActor_PassesThroughErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	want := errors.New("normal failure")

	actor := safeActor(logger, "test", func() error {
		return want
	})

	got := actor()
	if !errors.Is(got, want) {
		t.Errorf("err = %v, want %v", got, want)
	}
}

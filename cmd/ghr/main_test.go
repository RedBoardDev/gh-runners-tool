package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRunPrintsExecuteError(t *testing.T) {
	oldExecute := execute
	t.Cleanup(func() {
		execute = oldExecute
	})

	execute = func() error {
		return errors.New("boom")
	}

	var stderr bytes.Buffer
	code := run(&stderr)

	if code != 1 {
		t.Fatalf("run() exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "boom") {
		t.Fatalf("stderr = %q, want execute error", stderr.String())
	}
}

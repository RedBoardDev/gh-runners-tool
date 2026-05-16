package logging

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

type closingBuffer struct {
	bytes.Buffer
	closed bool
}

func (c *closingBuffer) Close() error {
	c.closed = true
	return nil
}

func decodeLines(t *testing.T, r io.Reader) []taggedLine {
	t.Helper()
	var out []taggedLine
	scanner := bytes.NewReader(make([]byte, 0))
	_ = scanner
	raw, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var tl taggedLine
		if err := json.Unmarshal([]byte(line), &tl); err != nil {
			t.Fatalf("decode %q: %v", line, err)
		}
		out = append(out, tl)
	}
	return out
}

func TestTaggedWriter_EmitsOneJSONLinePerNewline(t *testing.T) {
	buf := &closingBuffer{}
	w := newTaggedWriter(buf, "g1", "r1", "runner")

	if _, err := w.Write([]byte("hello\nworld\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	lines := decodeLines(t, bytes.NewReader(buf.Bytes()))
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if lines[0].Line != "hello" || lines[1].Line != "world" {
		t.Errorf("lines = %+v", lines)
	}
	for _, l := range lines {
		if l.Group != "g1" || l.Runner != "r1" || l.Source != "runner" {
			t.Errorf("missing tags on %+v", l)
		}
		if l.Time == "" {
			t.Errorf("missing timestamp on %+v", l)
		}
	}
}

func TestTaggedWriter_BuffersPartialLines(t *testing.T) {
	buf := &closingBuffer{}
	w := newTaggedWriter(buf, "g", "r", "runner")

	if _, err := w.Write([]byte("partial ")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("partial line should be buffered, wrote: %s", buf.String())
	}

	if _, err := w.Write([]byte("line\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	lines := decodeLines(t, bytes.NewReader(buf.Bytes()))
	if len(lines) != 1 || lines[0].Line != "partial line" {
		t.Errorf("got %+v, want 'partial line'", lines)
	}
}

func TestTaggedWriter_CloseFlushesTrailingPartial(t *testing.T) {
	buf := &closingBuffer{}
	w := newTaggedWriter(buf, "g", "r", "runner")

	if _, err := w.Write([]byte("orphan no newline")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	lines := decodeLines(t, bytes.NewReader(buf.Bytes()))
	if len(lines) != 1 || lines[0].Line != "orphan no newline" {
		t.Errorf("got %+v, want flushed orphan", lines)
	}
	if !buf.closed {
		t.Errorf("inner writer was not closed")
	}
}

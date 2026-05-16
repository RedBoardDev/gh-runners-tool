package logging

import (
	"bytes"
	"encoding/json"
	"io"
	"sync"
	"time"
)

// taggedWriter wraps an underlying io.WriteCloser and emits each line of input
// as a JSON object enriched with metadata (group, runner, source). Partial
// lines are buffered until a newline arrives, so structured tools can rely on
// one JSON object per output line.
type taggedWriter struct {
	mu     sync.Mutex
	inner  io.WriteCloser
	buf    bytes.Buffer
	group  string
	runner string
	now    func() time.Time
}

func newTaggedWriter(inner io.WriteCloser, group, runner string) *taggedWriter {
	return &taggedWriter{
		inner:  inner,
		group:  group,
		runner: runner,
		now:    time.Now,
	}
}

type taggedLine struct {
	Time   string `json:"time"`
	Source string `json:"source"`
	Group  string `json:"group"`
	Runner string `json:"runner"`
	Line   string `json:"line"`
}

func (w *taggedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)

	for {
		idx := bytes.IndexByte(w.buf.Bytes(), '\n')
		if idx < 0 {
			break
		}
		line := string(w.buf.Bytes()[:idx])
		w.buf.Next(idx + 1)
		if err := w.emit(line); err != nil {
			return 0, err
		}
	}

	return len(p), nil
}

func (w *taggedWriter) emit(line string) error {
	rec := taggedLine{
		Time:   w.now().UTC().Format(time.RFC3339Nano),
		Source: "runner",
		Group:  w.group,
		Runner: w.runner,
		Line:   line,
	}
	encoded, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	_, err = w.inner.Write(encoded)
	return err
}

func (w *taggedWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.buf.Len() > 0 {
		if err := w.emit(w.buf.String()); err != nil {
			w.inner.Close()
			return err
		}
		w.buf.Reset()
	}
	return w.inner.Close()
}

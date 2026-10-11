// Package storage is the persistence layer. The demo uses append-only JSON
// Lines files under the log directory instead of PostgreSQL/TimescaleDB; the
// engine only sees the TelemetryStore and Journal interfaces, so swapping in a
// database later does not touch business code.
package storage

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// jsonlWriter appends one JSON document per line. Safe for concurrent use.
type jsonlWriter struct {
	mu   sync.Mutex
	path string
	f    *os.File
}

func (w *jsonlWriter) write(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil || w.path != path {
		if w.f != nil {
			w.f.Close()
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		w.f, w.path = f, path
	}
	_, err = w.f.Write(append(b, '\n'))
	return err
}

func (w *jsonlWriter) close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

// readJSONL calls fn for every line of path. A missing file is not an error;
// malformed lines (e.g. a torn write after a crash) are skipped.
func readJSONL[T any](path string, fn func(T)) error {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var v T
		if json.Unmarshal(sc.Bytes(), &v) == nil {
			fn(v)
		}
	}
	return sc.Err()
}

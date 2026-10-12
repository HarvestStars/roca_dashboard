package storage

import "path/filepath"

// SnapshotLog is a generic append-only JSONL log of entity snapshots: every
// change appends the full entity, and ReadAll replays them in order so the
// caller keeps the last snapshot per id. Used by the ERP module.
type SnapshotLog[T any] struct {
	path string
	w    jsonlWriter
}

func NewSnapshotLog[T any](dir, name string) *SnapshotLog[T] {
	return &SnapshotLog[T]{path: filepath.Join(dir, name)}
}

func (l *SnapshotLog[T]) Append(v T) error { return l.w.write(l.path, v) }

func (l *SnapshotLog[T]) ReadAll(fn func(T)) error { return readJSONL(l.path, fn) }

func (l *SnapshotLog[T]) Close() error { return l.w.close() }

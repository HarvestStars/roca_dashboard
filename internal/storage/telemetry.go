package storage

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/harveststars/roca_dashboard/internal/model"
)

// TelemetryRecord is one line in logs/telemetry/YYYY-MM-DD.jsonl.
type TelemetryRecord struct {
	ReceivedAt time.Time       `json:"received_at"`
	Telemetry  model.Telemetry `json:"telemetry"`
}

// TelemetryStore is the time-series sink (TimescaleDB in production).
type TelemetryStore interface {
	Append(rec TelemetryRecord) error
	// Query returns records with from <= ReceivedAt < to, oldest first.
	Query(from, to time.Time) []TelemetryRecord
	Close() error
}

// TelemetryLog writes daily-rotated JSONL files and keeps a sliding in-memory
// window (the retention) for fast dashboard queries. On start it replays the
// files that overlap the window, so a restart does not blank the charts.
type TelemetryLog struct {
	dir       string
	retention time.Duration
	w         jsonlWriter

	mu   sync.RWMutex
	ring []TelemetryRecord
}

func NewTelemetryLog(logDir string, retention time.Duration) (*TelemetryLog, error) {
	t := &TelemetryLog{dir: filepath.Join(logDir, "telemetry"), retention: retention}
	cutoff := time.Now().Add(-retention)
	for d := cutoff; !d.After(time.Now().Add(24 * time.Hour)); d = d.Add(24 * time.Hour) {
		err := readJSONL(t.fileFor(d), func(r TelemetryRecord) {
			if r.ReceivedAt.After(cutoff) {
				t.ring = append(t.ring, r)
			}
		})
		if err != nil {
			return nil, err
		}
	}
	return t, nil
}

func (t *TelemetryLog) fileFor(ts time.Time) string {
	return filepath.Join(t.dir, ts.UTC().Format("2006-01-02")+".jsonl")
}

func (t *TelemetryLog) Append(rec TelemetryRecord) error {
	if err := t.w.write(t.fileFor(rec.ReceivedAt), rec); err != nil {
		return err
	}
	t.mu.Lock()
	t.ring = append(t.ring, rec)
	cutoff := rec.ReceivedAt.Add(-t.retention)
	i := 0
	for i < len(t.ring) && t.ring[i].ReceivedAt.Before(cutoff) {
		i++
	}
	// Re-slicing is enough: the next growth of the backing array copies only
	// the live tail, so the dropped head is reclaimed by the GC.
	t.ring = t.ring[i:]
	t.mu.Unlock()
	return nil
}

func (t *TelemetryLog) Query(from, to time.Time) []TelemetryRecord {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var out []TelemetryRecord
	for _, r := range t.ring {
		if !r.ReceivedAt.Before(from) && r.ReceivedAt.Before(to) {
			out = append(out, r)
		}
	}
	return out
}

func (t *TelemetryLog) Close() error { return t.w.close() }

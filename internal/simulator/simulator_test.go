package simulator

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/harveststars/roca_dashboard/internal/broker"
	"github.com/harveststars/roca_dashboard/internal/model"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func writeDevice(t *testing.T, path string, online bool, mod time.Time) {
	t.Helper()
	f := DeviceFile{Sim: SimControl{Online: &online}, Telemetry: model.Telemetry{StoreID: "S1", ControllerID: "C1"}}
	b, _ := json.Marshal(f)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	// explicit mtimes: two writes within the filesystem's timestamp resolution must still differ
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestOnlineToOfflineEmitsDisconnected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "S1_C1.json")
	bus := broker.NewMemory(quiet)
	defer bus.Close()
	events := make(chan model.DeviceEvent, 16)
	unsub, _ := bus.Subscribe(broker.Filter(broker.KindEvents), func(m broker.Message) {
		var ev model.DeviceEvent
		_ = json.Unmarshal(m.Payload, &ev)
		events <- ev
	})
	defer unsub()

	s := New(dir, bus, quiet)
	t0 := time.Now().Add(-time.Minute)
	writeDevice(t, path, true, t0)
	s.tick(time.Now()) // first load: no events

	writeDevice(t, path, false, t0.Add(time.Second))
	s.tick(time.Now())
	select {
	case ev := <-events:
		if ev.Type != model.EventDisconnected || ev.StoreID != "S1" || ev.ControllerID != "C1" {
			t.Fatalf("event=%+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no DISCONNECTED event")
	}

	// back online: a normal STATE_CHANGED, no second DISCONNECTED
	writeDevice(t, path, true, t0.Add(2*time.Second))
	s.tick(time.Now())
	select {
	case ev := <-events:
		if ev.Type != model.EventStateChanged {
			t.Fatalf("event=%+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no STATE_CHANGED event")
	}
}

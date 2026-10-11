// Package simulator stands in for the store controllers. Each JSON file in the
// devices directory is one controller: its content is the hardware state the
// controller reports. Editing a file is the demo equivalent of touching the
// hardware (unplugging a cable, killing the power, pulling the network).
package simulator

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"time"

	"github.com/harveststars/roca_dashboard/internal/broker"
	"github.com/harveststars/roca_dashboard/internal/model"
)

// DeviceFile is the on-disk format: a telemetry payload plus a "sim" block.
type DeviceFile struct {
	Sim SimControl `json:"sim"`
	model.Telemetry
}

type SimControl struct {
	Online      *bool `json:"online,omitempty"`       // false = box unplugged / no network (default true)
	IntervalSec int   `json:"interval_sec,omitempty"` // heartbeat period (default 10)
	Jitter      *bool `json:"jitter,omitempty"`       // add sensor noise so charts look alive (default true)
}

func (s SimControl) online() bool { return s.Online == nil || *s.Online }
func (s SimControl) jitter() bool { return s.Jitter == nil || *s.Jitter }
func (s SimControl) interval() time.Duration {
	if s.IntervalSec <= 0 {
		return 10 * time.Second
	}
	return time.Duration(s.IntervalSec) * time.Second
}

type device struct {
	path        string
	modTime     time.Time
	file        DeviceFile
	lastPublish time.Time
}

type Simulator struct {
	dir    string
	broker broker.Broker
	log    *slog.Logger
	scan   time.Duration

	devices map[string]*device // by path; only touched by the Run goroutine
	cmdKeys chan [2]string     // (store, controller) of pending SELF_TEST commands
}

func New(dir string, b broker.Broker, log *slog.Logger) *Simulator {
	return &Simulator{dir: dir, broker: b, log: log, scan: time.Second,
		devices: map[string]*device{}, cmdKeys: make(chan [2]string, 64)}
}

// Run polls the device directory once per second: a changed file is reported
// immediately (spec: "on state change"), otherwise each device heartbeats at
// its own interval. It also answers SELF_TEST commands from the cloud.
func (s *Simulator) Run(ctx context.Context) error {
	unsub, err := s.broker.Subscribe(broker.Filter(broker.KindCommands), func(m broker.Message) {
		store, ctrl, _, ok := broker.ParseTopic(m.Topic)
		if ok {
			select {
			case s.cmdKeys <- [2]string{store, ctrl}:
			default:
			}
		}
	})
	if err != nil {
		return err
	}
	defer unsub()

	s.tick(time.Now())
	t := time.NewTicker(s.scan)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-t.C:
			s.tick(now)
		case k := <-s.cmdKeys:
			s.selfTest(k[0], k[1])
		}
	}
}

func (s *Simulator) tick(now time.Time) {
	paths, _ := filepath.Glob(filepath.Join(s.dir, "*.json"))
	seen := map[string]bool{}
	for _, p := range paths {
		seen[p] = true
		changed, err := s.load(p)
		if err != nil {
			s.log.Warn("device file unreadable, skipping", "file", filepath.Base(p), "err", err)
			continue
		}
		d := s.devices[p]
		if changed {
			s.event(d, "STATE_CHANGED", "device file modified")
		}
		if changed || now.Sub(d.lastPublish) >= d.file.Sim.interval() {
			s.publish(d, now)
		}
	}
	for p := range s.devices {
		if !seen[p] {
			delete(s.devices, p) // deleted file == device removed; engine will flag it offline
		}
	}
}

// load (re)reads a device file when its mtime changed. Returns true on change.
func (s *Simulator) load(path string) (bool, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	d := s.devices[path]
	if d != nil && fi.ModTime().Equal(d.modTime) {
		return false, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var f DeviceFile
	if err := json.Unmarshal(b, &f); err != nil {
		return false, err
	}
	if d == nil {
		d = &device{path: path}
		s.devices[path] = d
		s.log.Info("device loaded", "file", filepath.Base(path), "store", f.StoreID, "controller", f.ControllerID, "online", f.Sim.online())
	}
	first := d.modTime.IsZero()
	d.file, d.modTime = f, fi.ModTime()
	return !first, nil
}

func (s *Simulator) selfTest(storeID, ctrlID string) {
	for p, d := range s.devices {
		if d.file.StoreID != storeID || d.file.ControllerID != ctrlID {
			continue
		}
		if _, err := s.load(p); err != nil {
			return
		}
		s.log.Info("self-test requested", "store", storeID, "controller", ctrlID, "online", d.file.Sim.online())
		s.publish(d, time.Now()) // offline devices stay silent => verify times out
		return
	}
}

func (s *Simulator) publish(d *device, now time.Time) {
	d.lastPublish = now
	if !d.file.Sim.online() {
		return
	}
	t := d.file.Telemetry // shallow copy; peripherals map is cloned below
	t.Timestamp = now.Unix()
	if t.ProtocolVersion == "" {
		t.ProtocolVersion = "1.0"
	}
	if d.file.Sim.jitter() {
		t = withJitter(t)
	}
	b, _ := json.Marshal(t)
	if err := s.broker.Publish(broker.Topic(t.StoreID, t.ControllerID, broker.KindTelemetry), b); err != nil {
		s.log.Warn("publish telemetry", "err", err)
	}
}

func (s *Simulator) event(d *device, typ, detail string) {
	if !d.file.Sim.online() {
		return
	}
	b, _ := json.Marshal(model.DeviceEvent{StoreID: d.file.StoreID, ControllerID: d.file.ControllerID,
		Timestamp: time.Now().Unix(), Type: typ, Detail: detail})
	_ = s.broker.Publish(broker.Topic(d.file.StoreID, d.file.ControllerID, broker.KindEvents), b)
}

// withJitter adds small sensor noise without changing the diagnostic outcome.
func withJitter(t model.Telemetry) model.Telemetry {
	t.Power.VoltageMv += rand.IntN(101) - 50
	t.Power.CurrentMa += rand.IntN(21) - 10
	t.Ambient.TempCelsius += float64(rand.IntN(7)-3) / 10
	per := make(map[string]model.Peripheral, len(t.Peripherals))
	for k, p := range t.Peripherals {
		if p.ImpedanceOhms != nil && *p.ImpedanceOhms < 1000 {
			v := *p.ImpedanceOhms + rand.IntN(3) - 1
			p.ImpedanceOhms = &v
		}
		per[k] = p
	}
	t.Peripherals = per
	return t
}

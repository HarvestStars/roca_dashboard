package diagnostics

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/harveststars/roca_dashboard/internal/broker"
	"github.com/harveststars/roca_dashboard/internal/model"
	"github.com/harveststars/roca_dashboard/internal/storage"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func ptr[T any](v T) *T { return &v }

func telemetry(loopOpen, antennaDead bool, mv int) model.Telemetry {
	loop := model.Peripheral{Port: "J2", ImpedanceOhms: ptr(6), Status: "CONNECTED"}
	if loopOpen {
		loop = model.Peripheral{Port: "J2", ImpedanceOhms: ptr(99999), Status: "OPEN_CIRCUIT"}
	}
	return model.Telemetry{
		StoreID: "S1", ControllerID: "C1",
		Power: model.Power{VoltageMv: mv, Status: "NORMAL"},
		Peripherals: map[string]model.Peripheral{
			"perimeter_loop_1":   loop,
			"checkout_antenna_2": {Port: "J4", ImpedanceOhms: ptr(50), Status: "CONNECTED", SignalTxActive: ptr(!antennaDead)},
		},
	}
}

type harness struct {
	t      *testing.T
	eng    *Engine
	bus    *broker.Memory
	dir    string
	cancel context.CancelFunc
}

func newHarness(t *testing.T, dir string) *harness {
	t.Helper()
	bus := broker.NewMemory(quiet)
	tel, err := storage.NewTelemetryLog(dir, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	j := storage.NewFileJournal(dir)
	stores := []model.Store{{StoreID: "S1", Name: "Store 1", Language: "en",
		Controllers: []model.ControllerInfo{{ControllerID: "C1", Model: "Box"}}}}
	eng, err := New(Config{OfflineTimeout: time.Hour, VerifyTimeout: 300 * time.Millisecond, WatchdogInterval: time.Hour, AvoidedTripCostEUR: 2000},
		stores, bus, tel, j, quiet)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go eng.Run(ctx)
	h := &harness{t: t, eng: eng, bus: bus, dir: dir, cancel: cancel}
	t.Cleanup(func() { cancel(); bus.Close(); tel.Close(); j.Close() })
	time.Sleep(20 * time.Millisecond) // let Run subscribe
	return h
}

func (h *harness) send(tm model.Telemetry) {
	b, _ := json.Marshal(tm)
	h.bus.Publish(broker.Topic(tm.StoreID, tm.ControllerID, broker.KindTelemetry), b)
	time.Sleep(30 * time.Millisecond)
}

func (h *harness) status() model.Status {
	v, _ := h.eng.Store("S1")
	return v.SystemStatus
}

func TestLoopIssueSelfResolved(t *testing.T) {
	h := newHarness(t, t.TempDir())
	h.send(telemetry(true, false, 12000))
	if s := h.status(); s != model.StatusActionRequired {
		t.Fatalf("status=%s", s)
	}
	v, _ := h.eng.Store("S1")
	if v.ActiveIssue.Finding.Code != model.RootCauseLoopDisconnected || v.ActiveIssue.Finding.TargetPort != "J2" {
		t.Fatalf("issue=%+v", v.ActiveIssue)
	}

	h.send(telemetry(false, false, 12000))
	if s := h.status(); s != model.StatusHealthy {
		t.Fatalf("status=%s", s)
	}
	sum := h.eng.Summary()
	if sum.IssuesSelfResolved != 1 || sum.EstSavingsEUR != 2000 {
		t.Fatalf("summary=%+v", sum)
	}
}

func TestHardwareFaultAutoEscalates(t *testing.T) {
	h := newHarness(t, t.TempDir())
	h.send(telemetry(false, true, 12000))
	if s := h.status(); s != model.StatusEscalated {
		t.Fatalf("status=%s", s)
	}
	tk := h.eng.Tickets()
	if len(tk) != 1 || tk[0].Source != model.TicketAuto || len(tk[0].RequiredParts) == 0 || len(tk[0].Snapshot) != 1 {
		t.Fatalf("tickets=%+v", tk)
	}
	h.send(telemetry(false, false, 12000))
	if sum := h.eng.Summary(); sum.IssuesResolvedByService != 1 || sum.TicketsOpen != 0 {
		t.Fatalf("summary=%+v", sum)
	}
}

func TestPowerTakesPriorityOverLoop(t *testing.T) {
	f := Evaluate(ptr(telemetry(true, false, 9000)), DefaultRules, time.Now())
	if len(f) == 0 || f[0].Code != model.RootCausePowerFailure {
		t.Fatalf("findings=%+v", f)
	}
}

func TestVerifyRoundTrip(t *testing.T) {
	h := newHarness(t, t.TempDir())
	h.send(telemetry(true, false, 12000))

	// Fake device: answers SELF_TEST with healthy telemetry.
	unsub, _ := h.bus.Subscribe(broker.Filter(broker.KindCommands), func(broker.Message) {
		b, _ := json.Marshal(telemetry(false, false, 12000))
		h.bus.Publish(broker.Topic("S1", "C1", broker.KindTelemetry), b)
	})
	res, err := h.eng.Verify(context.Background(), "S1", "", "")
	unsub()
	if err != nil || res.Result != "RESOLVED" || res.CurrentStatus != model.StatusHealthy {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestVerifyNoResponse(t *testing.T) {
	h := newHarness(t, t.TempDir())
	h.send(telemetry(true, false, 12000))
	res, _ := h.eng.Verify(context.Background(), "S1", "", "")
	if res.Result != "NO_RESPONSE" {
		t.Fatalf("res=%+v", res)
	}
}

func TestOfflineWatchdog(t *testing.T) {
	h := newHarness(t, t.TempDir())
	h.eng.cfg.OfflineTimeout = 0
	h.eng.checkOffline(time.Now().Add(time.Second))
	if s := h.status(); s != model.StatusOffline {
		t.Fatalf("status=%s", s)
	}
	h.send(telemetry(false, false, 12000))
	if s := h.status(); s != model.StatusHealthy {
		t.Fatalf("status=%s", s)
	}
}

func TestJournalReplayRestoresOpenIssue(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, dir)
	h.send(telemetry(true, false, 12000))
	h.cancel()

	h2 := newHarness(t, dir)
	v, _ := h2.eng.Store("S1")
	if v.ActiveIssue == nil || v.ActiveIssue.Finding.Code != model.RootCauseLoopDisconnected {
		t.Fatalf("after replay: %+v", v.ActiveIssue)
	}
	// New issues must not reuse ids from the journal.
	h2.send(telemetry(false, true, 12000))
	if iss := h2.eng.Issues(false); len(iss) != 2 || iss[0].IssueID == iss[1].IssueID {
		t.Fatalf("issues=%+v", iss)
	}
}

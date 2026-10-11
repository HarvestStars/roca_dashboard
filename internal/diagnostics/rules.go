// Package diagnostics is the core of the platform: it turns raw telemetry into
// root causes (rules.go) and tracks each controller through an issue
// lifecycle (engine.go). See docs/dashboard_demo_design.md §4.
package diagnostics

import (
	"fmt"
	"sort"
	"time"

	"github.com/harveststars/roca_dashboard/internal/model"
)

// Thresholds for the demo rule set.
const (
	MinVoltageMv    = 10000
	OpenCircuitOhms = 10000 // loop impedance above this is treated as an open circuit
	AntennaMinOhms  = 30
	AntennaMaxOhms  = 80
)

// Rule inspects one telemetry frame and returns zero or more findings.
// Rules are independent; Evaluate sorts the result by priority.
type Rule func(t *model.Telemetry, now time.Time) []model.Finding

var DefaultRules = []Rule{PowerRule, LoopRule, AntennaRule}

// priority of root causes when several fire at once: the first one is what
// the store manager is asked to fix first (e.g. power before loops, because
// a dead power supply also makes every loop look broken).
var priority = map[model.RootCauseCode]int{
	model.RootCauseNetworkOffline:   0,
	model.RootCausePowerFailure:     1,
	model.RootCauseLoopDisconnected: 2,
	model.RootCauseAntennaFault:     3,
}

func Evaluate(t *model.Telemetry, rules []Rule, now time.Time) []model.Finding {
	var out []model.Finding
	for _, r := range rules {
		out = append(out, r(t, now)...)
	}
	sort.SliceStable(out, func(i, j int) bool { return priority[out[i].Code] < priority[out[j].Code] })
	return out
}

func PowerRule(t *model.Telemetry, now time.Time) []model.Finding {
	if t.Power.Status == "NORMAL" && t.Power.VoltageMv >= MinVoltageMv {
		return nil
	}
	return []model.Finding{{
		Code:          model.RootCausePowerFailure,
		Severity:      model.SeverityCritical,
		TargetModule:  "Power Adapter Unit",
		IsSelfFixable: true,
		ActionHint:    fmt.Sprintf("Supply at %d mV (status %s). Check power adapter outlet connection or fuse.", t.Power.VoltageMv, t.Power.Status),
		RequiredParts: []string{"PSU-12V-5A"},
		DetectedAt:    now,
	}}
}

// LoopRule: power is fine but a perimeter loop reads as an open circuit, which
// in the field is almost always a loose connector, i.e. self-fixable.
func LoopRule(t *model.Telemetry, now time.Time) []model.Finding {
	if t.Power.Status != "NORMAL" || t.Power.VoltageMv < MinVoltageMv {
		return nil // power rule owns this case
	}
	var out []model.Finding
	for _, name := range sortedKeys(t.Peripherals) {
		p := t.Peripherals[name]
		if p.Kind(name) != model.PeripheralLoop {
			continue
		}
		open := p.Status == "OPEN_CIRCUIT" || (p.ImpedanceOhms != nil && *p.ImpedanceOhms > OpenCircuitOhms)
		if !open {
			continue
		}
		out = append(out, model.Finding{
			Code:          model.RootCauseLoopDisconnected,
			Severity:      model.SeverityHigh,
			TargetModule:  fmt.Sprintf("%s connector %s", name, p.Port),
			TargetPort:    p.Port,
			IsSelfFixable: true,
			ActionHint:    fmt.Sprintf("Check physical screw-terminal or plug at port %s behind the master control box.", p.Port),
			DetectedAt:    now,
		})
	}
	return out
}

// AntennaRule: antenna is powered and wired but does not transmit, or its
// impedance drifted out of band — a component failure, needs a technician.
func AntennaRule(t *model.Telemetry, now time.Time) []model.Finding {
	var out []model.Finding
	for _, name := range sortedKeys(t.Peripherals) {
		p := t.Peripherals[name]
		if p.Kind(name) != model.PeripheralAntenna {
			continue
		}
		txDead := p.SignalTxActive != nil && !*p.SignalTxActive
		outOfBand := p.ImpedanceOhms != nil && (*p.ImpedanceOhms < AntennaMinOhms || *p.ImpedanceOhms > AntennaMaxOhms)
		if !(p.Status == "FAULT" || txDead || outOfBand) {
			continue
		}
		out = append(out, model.Finding{
			Code:          model.RootCauseAntennaFault,
			Severity:      model.SeverityHigh,
			TargetModule:  fmt.Sprintf("%s (port %s)", name, p.Port),
			TargetPort:    p.Port,
			IsSelfFixable: false,
			ActionHint:    "Antenna TX stage not transmitting. Replace antenna driver board.",
			RequiredParts: []string{"ANT-DRV-BOARD-V3", "COAX-RG58-2M"},
			DetectedAt:    now,
		})
	}
	return out
}

// OfflineFinding is produced by the engine's watchdog, not by a telemetry rule.
func OfflineFinding(lastSeen, now time.Time) model.Finding {
	hint := "No telemetry received since engine start."
	if !lastSeen.IsZero() {
		hint = fmt.Sprintf("No telemetry since %s. Check controller power and network cable.", lastSeen.UTC().Format(time.RFC3339))
	}
	return model.Finding{
		Code:          model.RootCauseNetworkOffline,
		Severity:      model.SeverityCritical,
		TargetModule:  "Controller uplink (LAN)",
		TargetPort:    "LAN",
		IsSelfFixable: true,
		ActionHint:    hint,
		DetectedAt:    now,
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

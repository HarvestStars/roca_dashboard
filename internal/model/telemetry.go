// Package model holds the wire formats and domain types shared by every module.
// Field names follow docs/technical_architecture_spec.md §2.3.
package model

import "strings"

// Telemetry is the payload a store controller publishes on
// rocateq/store/{store_id}/controller/{controller_id}/telemetry.
type Telemetry struct {
	ProtocolVersion string                `json:"protocol_version"`
	StoreID         string                `json:"store_id"`
	ControllerID    string                `json:"controller_id"`
	Timestamp       int64                 `json:"timestamp"` // unix seconds
	FirmwareVersion string                `json:"firmware_version"`
	Power           Power                 `json:"power"`
	Peripherals     map[string]Peripheral `json:"peripherals"`
	Ambient         Ambient               `json:"ambient"`
}

type Power struct {
	VoltageMv int    `json:"voltage_mv"`
	CurrentMa int    `json:"current_ma"`
	Status    string `json:"status"` // NORMAL | UNDERVOLTAGE | OVERVOLTAGE | ...
}

// Peripheral is one device wired to the controller (loop, antenna, relay).
// Type and Port are demo extensions to the spec: Type is inferred from the
// peripheral name when empty, Port names the physical socket (e.g. "J2").
type Peripheral struct {
	Type           string `json:"type,omitempty"`
	Port           string `json:"port,omitempty"`
	ImpedanceOhms  *int   `json:"impedance_ohms,omitempty"`
	Status         string `json:"status"`
	SignalTxActive *bool  `json:"signal_tx_active,omitempty"`
}

const (
	PeripheralLoop    = "loop"
	PeripheralAntenna = "antenna"
	PeripheralRelay   = "relay"
	PeripheralOther   = "other"
)

// Kind returns the peripheral type, falling back to the naming convention
// used in the spec (perimeter_loop_1, checkout_antenna_2, cctv_trigger_relay).
func (p Peripheral) Kind(name string) string {
	if p.Type != "" {
		return p.Type
	}
	switch n := strings.ToLower(name); {
	case strings.Contains(n, "loop"):
		return PeripheralLoop
	case strings.Contains(n, "antenna"):
		return PeripheralAntenna
	case strings.Contains(n, "relay"):
		return PeripheralRelay
	}
	return PeripheralOther
}

type Ambient struct {
	TempCelsius float64 `json:"temp_celsius"`
}

// DeviceEvent is published on .../events for sudden state changes.
type DeviceEvent struct {
	StoreID      string `json:"store_id"`
	ControllerID string `json:"controller_id"`
	Timestamp    int64  `json:"timestamp"`
	Type         string `json:"type"`
	Detail       string `json:"detail,omitempty"`
}

// Command is sent by the cloud on .../commands.
type Command struct {
	Command   string `json:"command"` // SELF_TEST | RESET
	RequestID string `json:"request_id"`
	IssuedAt  int64  `json:"issued_at"`
}

const CommandSelfTest = "SELF_TEST"

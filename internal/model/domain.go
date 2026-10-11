package model

import "time"

// ---- Store registry -------------------------------------------------------

// Store is a customer site. Loaded from data/stores.json.
type Store struct {
	StoreID     string           `json:"store_id"`
	Name        string           `json:"name"`
	Chain       string           `json:"chain"`
	City        string           `json:"city"`
	Country     string           `json:"country"`
	Language    string           `json:"language"` // preferred wizard language: en | nl | fr | de
	Latitude    float64          `json:"latitude"`
	Longitude   float64          `json:"longitude"`
	Controllers []ControllerInfo `json:"controllers"`
}

type ControllerInfo struct {
	ControllerID string `json:"controller_id"`
	Model        string `json:"model"`
	Location     string `json:"location"` // written in the store's language, shown to the manager
}

// ---- Diagnostics ----------------------------------------------------------

type RootCauseCode string

const (
	RootCauseOk               RootCauseCode = "SYS_NORMAL"
	RootCauseLoopDisconnected RootCauseCode = "LOOP_CABLE_DISCONNECTED"
	RootCausePowerFailure     RootCauseCode = "POWER_SUPPLY_ABNORMAL"
	RootCauseAntennaFault     RootCauseCode = "ANTENNA_TX_FAULT"
	RootCauseNetworkOffline   RootCauseCode = "GATEWAY_OFFLINE"
	RootCauseManagerReported  RootCauseCode = "MANAGER_REPORTED"
)

type Severity string

const (
	SeverityInfo     Severity = "INFO"
	SeverityHigh     Severity = "HIGH"
	SeverityCritical Severity = "CRITICAL"
)

// Finding is the output of one diagnostic rule.
type Finding struct {
	Code          RootCauseCode `json:"code"`
	Severity      Severity      `json:"severity"`
	TargetModule  string        `json:"target_module"`
	TargetPort    string        `json:"target_port,omitempty"`
	IsSelfFixable bool          `json:"is_self_fixable"`
	ActionHint    string        `json:"action_hint"`
	RequiredParts []string      `json:"required_parts,omitempty"`
	DetectedAt    time.Time     `json:"detected_at"`
}

// ---- Status ---------------------------------------------------------------

type Status string

const (
	StatusHealthy         Status = "HEALTHY"
	StatusUnknown         Status = "UNKNOWN"          // no report yet, offline timer not expired
	StatusActionRequired  Status = "ACTION_REQUIRED"  // store manager can fix it
	StatusEscalated       Status = "ESCALATED"        // ticket open, Rocateq is on it
	StatusServiceRequired Status = "SERVICE_REQUIRED" // hardware fault, not yet escalated
	StatusOffline         Status = "OFFLINE"
)

// Rank orders statuses by urgency; the store shows the worst controller.
func (s Status) Rank() int {
	switch s {
	case StatusHealthy:
		return 0
	case StatusUnknown:
		return 1
	case StatusActionRequired:
		return 2
	case StatusEscalated:
		return 3
	case StatusServiceRequired:
		return 4
	case StatusOffline:
		return 5
	}
	return 1
}

// ---- Issues & tickets -----------------------------------------------------

type IssueState string

const (
	IssueOpen      IssueState = "OPEN"
	IssueEscalated IssueState = "ESCALATED"
	IssueResolved  IssueState = "RESOLVED"
)

type Resolution string

const (
	ResolutionSelf       Resolution = "SELF_RESOLVED"       // fixed by the store, no trip needed
	ResolutionService    Resolution = "RESOLVED_BY_SERVICE" // fixed after escalation
	ResolutionSuperseded Resolution = "SUPERSEDED"          // replaced by a different root cause
)

// Issue is one occurrence of a root cause on one controller, from detection
// to recovery.
type Issue struct {
	IssueID        string     `json:"issue_id"`
	StoreID        string     `json:"store_id"`
	ControllerID   string     `json:"controller_id"`
	Finding        Finding    `json:"finding"`
	State          IssueState `json:"state"`
	OpenedAt       time.Time  `json:"opened_at"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	Resolution     Resolution `json:"resolution,omitempty"`
	VerifyAttempts int        `json:"verify_attempts"`
	TicketID       string     `json:"ticket_id,omitempty"`
}

type TicketSource string

const (
	TicketAuto    TicketSource = "AUTO"          // engine decided it is a hardware fault
	TicketManager TicketSource = "STORE_MANAGER" // manager pressed "request support"
)

// Ticket is a qualified dispatch request: it carries everything an engineer
// needs to decide whether to travel and which spare parts to bring.
type Ticket struct {
	TicketID       string        `json:"ticket_id"`
	StoreID        string        `json:"store_id"`
	ControllerID   string        `json:"controller_id"`
	IssueID        string        `json:"issue_id,omitempty"`
	Code           RootCauseCode `json:"code"`
	Source         TicketSource  `json:"source"`
	Status         string        `json:"status"` // OPEN | CLOSED
	CreatedAt      time.Time     `json:"created_at"`
	ClosedAt       *time.Time    `json:"closed_at,omitempty"`
	RequiredParts  []string      `json:"required_parts,omitempty"`
	WizardAttempts int           `json:"wizard_attempts"`
	Note           string        `json:"note,omitempty"`
	Snapshot       []Telemetry   `json:"snapshot,omitempty"` // last telemetry of every controller in the store
}

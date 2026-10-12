// Package erp implements requirement 2 of the spec: dispatching a qualified
// ticket as an engineer trip, on-site check-in/out timesheets, receipt photos
// parsed by a vision LLM, and trip approval. See docs/erp_demo_design.md.
package erp

import (
	"time"

	"github.com/harveststars/roca_dashboard/internal/llm"
)

type Engineer struct {
	EngineerID string `json:"engineer_id"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	HomeBase   string `json:"home_base"`
}

// ---- Trips & timesheets ---------------------------------------------------

type TripStatus string

const (
	TripAssigned  TripStatus = "ASSIGNED"  // dispatched, engineer not on site yet
	TripOnSite    TripStatus = "ON_SITE"   // last punch is CHECK_IN
	TripWorkDone  TripStatus = "WORK_DONE" // last punch is CHECK_OUT
	TripSubmitted TripStatus = "SUBMITTED" // engineer submitted hours + receipts
	TripApproved  TripStatus = "APPROVED"
)

const (
	ActionCheckIn  = "CHECK_IN"
	ActionCheckOut = "CHECK_OUT"
)

type GeoPoint struct {
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	AccuracyM float64 `json:"accuracy_m,omitempty"`
}

// Punch is one check-in or check-out. At is server time (authoritative);
// the client clock is kept for audit only.
type Punch struct {
	Action          string    `json:"action"`
	At              time.Time `json:"at"`
	ClientTimestamp int64     `json:"client_timestamp,omitempty"`
	Geo             *GeoPoint `json:"geo,omitempty"`
	DistanceKm      *float64  `json:"distance_to_store_km,omitempty"`
}

type Trip struct {
	TripID         string     `json:"trip_id"`
	TicketID       string     `json:"ticket_id"`
	IssueID        string     `json:"issue_id,omitempty"`
	StoreID        string     `json:"store_id"`
	StoreName      string     `json:"store_name"`
	City           string     `json:"city"`
	Country        string     `json:"country"`
	StoreLat       float64    `json:"store_lat"`
	StoreLng       float64    `json:"store_lng"`
	ControllerID   string     `json:"controller_id"`
	Code           string     `json:"code"`
	RequiredParts  []string   `json:"required_parts,omitempty"`
	EngineerID     string     `json:"engineer_id"`
	Status         TripStatus `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	Punches        []Punch    `json:"punches"`
	ReminderSentAt *time.Time `json:"reminder_sent_at,omitempty"`
	SubmittedAt    *time.Time `json:"submitted_at,omitempty"`
	ApprovedAt     *time.Time `json:"approved_at,omitempty"`
	ApprovedBy     string     `json:"approved_by,omitempty"`
}

// ---- Expenses -------------------------------------------------------------

type ExpenseStatus string

const (
	ExpenseAnalyzing   ExpenseStatus = "ANALYZING"    // LLM call in progress
	ExpenseExtracted   ExpenseStatus = "EXTRACTED"    // LLM result accepted automatically
	ExpenseNeedsReview ExpenseStatus = "NEEDS_REVIEW" // LLM unsure: engineer must check
	ExpenseConfirmed   ExpenseStatus = "CONFIRMED"    // engineer confirmed or corrected
	ExpenseFailed      ExpenseStatus = "FAILED"       // LLM unavailable/failed: manual entry
)

// AutoAcceptConfidence: below this an extraction needs a human look.
const AutoAcceptConfidence = 0.8

type Expense struct {
	ExpenseID  string        `json:"expense_id"`
	TripID     string        `json:"trip_id"`
	EngineerID string        `json:"engineer_id"`
	ImageFile  string        `json:"image_file"`
	ImageMime  string        `json:"image_mime"`
	SourceName string        `json:"source_name,omitempty"` // original file name
	UploadedAt time.Time     `json:"uploaded_at"`
	Status     ExpenseStatus `json:"status"`
	// Extracted is the raw model output and is never edited, so the audit
	// trail always shows what the LLM read vs. what the engineer approved.
	Extracted        *llm.Receipt `json:"extracted,omitempty"`
	Final            *llm.Receipt `json:"final,omitempty"`
	EditedByEngineer bool         `json:"edited_by_engineer"`
	Model            string       `json:"model,omitempty"`
	LatencyMs        int64        `json:"latency_ms,omitempty"`
	Error            string       `json:"error,omitempty"`
}

// AutoAccepted reports whether the AI result went through without a human
// having to look at or change it.
func (e Expense) AutoAccepted() bool {
	return e.Extracted != nil && reviewStatus(e.Extracted) == ExpenseExtracted && !e.EditedByEngineer
}

// Pending reports whether the expense still blocks approval.
func (e Expense) Pending() bool {
	return e.Status == ExpenseAnalyzing || e.Status == ExpenseNeedsReview || e.Status == ExpenseFailed
}

// ---- Outbox ---------------------------------------------------------------

type Email struct {
	EmailID string    `json:"email_id"`
	Kind    string    `json:"kind"` // RECEIPT_REMINDER | RECEIPTS_RECEIVED | TRIP_APPROVED
	TripID  string    `json:"trip_id"`
	To      string    `json:"to"`
	Subject string    `json:"subject"`
	Body    string    `json:"body"`
	SentAt  time.Time `json:"sent_at"`
}

// ---- Read models ----------------------------------------------------------

// TripSummary follows spec §3.4 #2, plus the receipt time envelope used as
// input for the (open) hours-calibration discussion.
type TripSummary struct {
	TripID           string        `json:"trip_id"`
	EngineerName     string        `json:"engineer_name"`
	Destination      string        `json:"destination"`
	TotalOnsiteHours float64       `json:"total_onsite_hours"`
	OnSiteNow        bool          `json:"on_site_now"`
	TotalTravelHours *float64      `json:"total_travel_hours"` // null: not computed in v1, see design doc
	Expenses         ExpenseTotals `json:"expenses"`
	ReceiptEnvelope  *TimeEnvelope `json:"receipt_envelope,omitempty"`
	GeoWarnings      int           `json:"geo_warnings"`
	Status           string        `json:"status"` // IN_PROGRESS | AWAITING_RECEIPT_REVIEW | READY_FOR_APPROVAL | APPROVED
	TripStatus       TripStatus    `json:"trip_status"`
}

type ExpenseTotals struct {
	TotalEUR              float64            `json:"total_eur"`
	VATEUR                float64            `json:"vat_eur"`
	ReceiptCount          int                `json:"receipt_count"`
	ReceiptsPendingReview int                `json:"receipts_pending_review"`
	ByCategory            map[string]float64 `json:"by_category"`
}

// TimeEnvelope is the earliest and latest moment printed on the trip's
// receipts (e.g. outbound departure .. return arrival).
type TimeEnvelope struct {
	First     string  `json:"first"`
	Last      string  `json:"last"`
	SpanHours float64 `json:"span_hours"`
}

type TripDetail struct {
	Trip     Trip        `json:"trip"`
	Engineer Engineer    `json:"engineer"`
	Summary  TripSummary `json:"summary"`
	Expenses []Expense   `json:"expenses"`
}

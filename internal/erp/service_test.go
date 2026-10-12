package erp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/harveststars/roca_dashboard/internal/diagnostics"
	"github.com/harveststars/roca_dashboard/internal/llm"
	"github.com/harveststars/roca_dashboard/internal/model"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type fakeTickets struct{}

func (fakeTickets) Tickets() []model.Ticket {
	return []model.Ticket{{TicketID: "TCK-1", StoreID: "DE-1", ControllerID: "C1", Code: "ANTENNA_TX_FAULT",
		Status: "OPEN", RequiredParts: []string{"ANT-DRV-BOARD-V3"}}}
}

func (fakeTickets) Store(id string) (diagnostics.StoreView, bool) {
	return diagnostics.StoreView{StoreID: id, Name: "REWE Berlin", City: "Berlin", Country: "DE", Latitude: 52.52, Longitude: 13.405}, id == "DE-1"
}

type fakeExtractor struct {
	r   *llm.Receipt
	err error
}

func (fakeExtractor) Name() string { return "fake" }
func (f fakeExtractor) ExtractReceipt(context.Context, []byte, string) (*llm.Receipt, error) {
	if f.err != nil {
		return nil, f.err
	}
	cp := *f.r
	return &cp, nil
}

// smallest valid PNG header is enough for content sniffing
var png = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR")

func newSvc(t *testing.T, dir string, ex llm.ReceiptExtractor) *Service {
	t.Helper()
	s, err := New(Config{UploadDir: dir + "/up", PublicBaseURL: "http://x", ReminderDelay: 10 * time.Millisecond, ExtractTimeout: time.Second},
		[]Engineer{{EngineerID: "ENG-1", Name: "Mark de Boer", Email: "mark@example.com"}}, fakeTickets{}, ex, dir, quiet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func waitExpense(t *testing.T, s *Service, tripID string) Expense {
	t.Helper()
	for range 100 {
		d, _ := s.Trip(tripID)
		if len(d.Expenses) > 0 && d.Expenses[0].Status != ExpenseAnalyzing {
			return d.Expenses[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("extraction did not finish")
	return Expense{}
}

func f(v float64) *float64 { return &v }

func TestTripLifecycle(t *testing.T) {
	good := &llm.Receipt{MerchantName: "Hotel", TransactionDate: "2026-10-12", Currency: "EUR", TotalAmount: 153.45,
		VATAmount: 11.31, ExpenseCategory: "HOTEL", ConfidenceScore: 0.95}
	s := newSvc(t, t.TempDir(), fakeExtractor{r: good})

	if q := s.DispatchQueue(); len(q) != 1 {
		t.Fatalf("queue=%v", q)
	}
	trip, err := s.Dispatch("TCK-1", "ENG-1")
	if err != nil || trip.Status != TripAssigned {
		t.Fatalf("trip=%+v err=%v", trip, err)
	}
	if len(s.DispatchQueue()) != 0 {
		t.Fatal("dispatched ticket still queued")
	}
	if _, err := s.Dispatch("TCK-1", "ENG-1"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("double dispatch err=%v", err)
	}

	if _, err := s.Punch(PunchRequest{EngineerID: "ENG-1", TripID: trip.TripID, Action: ActionCheckOut}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("check-out before check-in err=%v", err)
	}
	// Check in ~200 km away from the store => geo warning.
	if _, err := s.Punch(PunchRequest{EngineerID: "ENG-1", TripID: trip.TripID, Action: ActionCheckIn, GeoLat: f(52.37), GeoLng: f(10.0)}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if tr, err := s.Punch(PunchRequest{EngineerID: "ENG-1", TripID: trip.TripID, Action: ActionCheckOut}); err != nil || tr.Status != TripWorkDone {
		t.Fatalf("tr=%+v err=%v", tr, err)
	}
	time.Sleep(50 * time.Millisecond) // reminder fires after ReminderDelay

	if _, err := s.AddExpense(trip.TripID, "ENG-1", png, "hotel.png"); err != nil {
		t.Fatal(err)
	}
	if e := waitExpense(t, s, trip.TripID); e.Status != ExpenseExtracted || e.Final.TotalAmount != 153.45 {
		t.Fatalf("expense=%+v", e)
	}

	if _, err := s.Approve(trip.TripID, "Derrick"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("approve before submit err=%v", err)
	}
	if _, err := s.Submit(trip.TripID, "ENG-1"); err != nil {
		t.Fatal(err)
	}
	d, _ := s.Trip(trip.TripID)
	if d.Summary.Status != "READY_FOR_APPROVAL" || d.Summary.Expenses.TotalEUR != 153.45 || d.Summary.GeoWarnings != 1 || d.Summary.TotalOnsiteHours < 0 {
		t.Fatalf("summary=%+v", d.Summary)
	}
	if tr, err := s.Approve(trip.TripID, "Derrick"); err != nil || tr.Status != TripApproved {
		t.Fatalf("tr=%+v err=%v", tr, err)
	}

	kinds := map[string]bool{}
	for _, m := range s.Outbox() {
		kinds[m.Kind] = true
	}
	if !kinds["RECEIPT_REMINDER"] || !kinds["RECEIPTS_RECEIVED"] || !kinds["TRIP_APPROVED"] {
		t.Fatalf("emails=%v", kinds)
	}
}

func TestLowConfidenceBlocksApprovalUntilConfirmed(t *testing.T) {
	unsure := &llm.Receipt{MerchantName: "Parkhaus", TransactionDate: "2026-10-12", TotalAmount: 12, ExpenseCategory: "OTHER", ConfidenceScore: 0.4}
	s := newSvc(t, t.TempDir(), fakeExtractor{r: unsure})
	trip, _ := s.Dispatch("TCK-1", "ENG-1")
	s.Punch(PunchRequest{EngineerID: "ENG-1", TripID: trip.TripID, Action: ActionCheckIn})
	s.Punch(PunchRequest{EngineerID: "ENG-1", TripID: trip.TripID, Action: ActionCheckOut})
	s.AddExpense(trip.TripID, "ENG-1", png, "parking.png")
	e := waitExpense(t, s, trip.TripID)
	if e.Status != ExpenseNeedsReview {
		t.Fatalf("status=%s", e.Status)
	}
	s.Submit(trip.TripID, "ENG-1")
	if _, err := s.Approve(trip.TripID, ""); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("approve with pending review err=%v", err)
	}
	fixed := *e.Final
	fixed.TotalAmount = 14
	if c, err := s.ConfirmExpense(e.ExpenseID, "ENG-1", &fixed); err != nil || !c.EditedByEngineer || c.Extracted.TotalAmount != 12 {
		t.Fatalf("c=%+v err=%v", c, err)
	}
	if _, err := s.Approve(trip.TripID, ""); err != nil {
		t.Fatal(err)
	}
}

func TestNoVisionModelGoesToManualEntry(t *testing.T) {
	s := newSvc(t, t.TempDir(), llm.NoVision{})
	trip, _ := s.Dispatch("TCK-1", "ENG-1")
	s.AddExpense(trip.TripID, "ENG-1", png, "x.png")
	e := waitExpense(t, s, trip.TripID)
	if e.Status != ExpenseFailed || e.Final != nil {
		t.Fatalf("expense=%+v", e)
	}
}

func TestRejectsNonImageUpload(t *testing.T) {
	s := newSvc(t, t.TempDir(), llm.NoVision{})
	trip, _ := s.Dispatch("TCK-1", "ENG-1")
	if _, err := s.AddExpense(trip.TripID, "ENG-1", []byte("<html>hi</html>"), "x.html"); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("err=%v", err)
	}
}

func TestReplayRestoresState(t *testing.T) {
	dir := t.TempDir()
	s := newSvc(t, dir, llm.NoVision{})
	trip, _ := s.Dispatch("TCK-1", "ENG-1")
	s.Punch(PunchRequest{EngineerID: "ENG-1", TripID: trip.TripID, Action: ActionCheckIn})
	s.Close()

	s2 := newSvc(t, dir, llm.NoVision{})
	d, err := s2.Trip(trip.TripID)
	if err != nil || d.Trip.Status != TripOnSite || len(d.Trip.Punches) != 1 {
		t.Fatalf("d=%+v err=%v", d.Trip, err)
	}
	if _, err := s2.Dispatch("TCK-1", "ENG-1"); !errors.Is(err, ErrInvalidState) {
		t.Fatal("replayed trip should block re-dispatch")
	}
}

func TestReceiptEnvelope(t *testing.T) {
	env := receiptEnvelope([]Expense{
		{Final: &llm.Receipt{TransactionDate: "2026-10-12", TravelSegment: &llm.TravelSegment{DepartureTime: "2026-10-12T07:10", ArrivalTime: "2026-10-12T08:35"}}},
		{Final: &llm.Receipt{TransactionDate: "2026-10-13", TravelSegment: &llm.TravelSegment{DepartureTime: "2026-10-13T17:45", ArrivalTime: "2026-10-13T19:05"}}},
	})
	if env == nil || env.First != "2026-10-12 07:10" || env.Last != "2026-10-13 19:05" || env.SpanHours != 35.92 {
		t.Fatalf("env=%+v", env)
	}
}

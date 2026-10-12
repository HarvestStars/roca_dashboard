package erp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/harveststars/roca_dashboard/internal/diagnostics"
	"github.com/harveststars/roca_dashboard/internal/llm"
	"github.com/harveststars/roca_dashboard/internal/model"
	"github.com/harveststars/roca_dashboard/internal/storage"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrInvalidState = errors.New("invalid state")
	ErrBadRequest   = errors.New("bad request")
)

// TicketSource is what ERP needs from the diagnostics engine.
type TicketSource interface {
	Tickets() []model.Ticket
	Store(storeID string) (diagnostics.StoreView, bool)
}

type Config struct {
	UploadDir      string
	PublicBaseURL  string
	ReminderDelay  time.Duration
	ExtractTimeout time.Duration
	MaxUploadBytes int64
}

type Service struct {
	cfg       Config
	tickets   TicketSource
	extractor llm.ReceiptExtractor
	log       *slog.Logger

	tripLog    *storage.SnapshotLog[Trip]
	expenseLog *storage.SnapshotLog[Expense]
	outboxLog  *storage.SnapshotLog[Email]

	mu         sync.Mutex
	engineers  map[string]Engineer
	engOrder   []string
	trips      map[string]*Trip
	tripOrder  []string
	expenses   map[string]*Expense
	expOrder   []string
	emails     []Email
	seq        map[string]int // per id prefix
	timers     map[string]*time.Timer
	closed     bool
	background sync.WaitGroup
}

func New(cfg Config, engineers []Engineer, tickets TicketSource, extractor llm.ReceiptExtractor, logDir string, log *slog.Logger) (*Service, error) {
	if cfg.MaxUploadBytes == 0 {
		cfg.MaxUploadBytes = 10 << 20
	}
	dir := filepath.Join(logDir, "erp")
	s := &Service{
		cfg: cfg, tickets: tickets, extractor: extractor, log: log,
		tripLog:    storage.NewSnapshotLog[Trip](dir, "trips.jsonl"),
		expenseLog: storage.NewSnapshotLog[Expense](dir, "expenses.jsonl"),
		outboxLog:  storage.NewSnapshotLog[Email](dir, "outbox.jsonl"),
		engineers:  map[string]Engineer{}, trips: map[string]*Trip{}, expenses: map[string]*Expense{},
		seq: map[string]int{}, timers: map[string]*time.Timer{},
	}
	for _, e := range engineers {
		s.engineers[e.EngineerID] = e
		s.engOrder = append(s.engOrder, e.EngineerID)
	}
	if err := os.MkdirAll(filepath.Join(cfg.UploadDir, "receipts"), 0o755); err != nil {
		return nil, err
	}
	if err := s.replay(); err != nil {
		return nil, fmt.Errorf("erp replay: %w", err)
	}
	return s, nil
}

func (s *Service) replay() error {
	if err := s.tripLog.ReadAll(func(t Trip) {
		if _, ok := s.trips[t.TripID]; !ok {
			s.tripOrder = append(s.tripOrder, t.TripID)
		}
		s.trips[t.TripID] = &t // t is a fresh copy per call
		s.bumpSeq(t.TripID)
	}); err != nil {
		return err
	}
	if err := s.expenseLog.ReadAll(func(e Expense) {
		if _, ok := s.expenses[e.ExpenseID]; !ok {
			s.expOrder = append(s.expOrder, e.ExpenseID)
		}
		s.expenses[e.ExpenseID] = &e
		s.bumpSeq(e.ExpenseID)
	}); err != nil {
		return err
	}
	if err := s.outboxLog.ReadAll(func(m Email) { s.emails = append(s.emails, m); s.bumpSeq(m.EmailID) }); err != nil {
		return err
	}
	// Resume work interrupted by a restart.
	for _, t := range s.trips {
		if t.Status == TripWorkDone && t.ReminderSentAt == nil {
			s.scheduleReminderLocked(t.TripID, s.cfg.ReminderDelay)
		}
	}
	for _, e := range s.expenses {
		if e.Status == ExpenseAnalyzing {
			s.startExtraction(e.ExpenseID)
		}
	}
	s.log.Info("erp replayed", "trips", len(s.trips), "expenses", len(s.expenses), "emails", len(s.emails))
	return nil
}

// ids look like PREFIX-...-NNNN; keep a counter per prefix so restarts never reuse one.
func (s *Service) bumpSeq(id string) {
	prefix, _, _ := strings.Cut(id, "-")
	n, _ := strconv.Atoi(id[strings.LastIndex(id, "-")+1:])
	s.seq[prefix] = max(s.seq[prefix], n)
}

func (s *Service) nextID(prefix, middle string) string {
	s.seq[prefix]++
	return fmt.Sprintf("%s-%s-%04d", prefix, middle, s.seq[prefix])
}

// ---- Engineers & dispatch -------------------------------------------------

func (s *Service) Engineers() []Engineer {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Engineer, 0, len(s.engOrder))
	for _, id := range s.engOrder {
		out = append(out, s.engineers[id])
	}
	return out
}

// DispatchQueue returns open diagnostics tickets that have no trip yet.
func (s *Service) DispatchQueue() []model.Ticket {
	tickets := s.tickets.Tickets()
	s.mu.Lock()
	defer s.mu.Unlock()
	dispatched := map[string]bool{}
	for _, t := range s.trips {
		dispatched[t.TicketID] = true
	}
	var out []model.Ticket
	for _, t := range tickets {
		if t.Status == "OPEN" && !dispatched[t.TicketID] {
			t.Snapshot = nil // not needed by the dispatcher UI
			out = append(out, t)
		}
	}
	return out
}

// Dispatch turns a qualified ticket into a trip for one engineer.
func (s *Service) Dispatch(ticketID, engineerID string) (Trip, error) {
	var tk *model.Ticket
	for _, t := range s.tickets.Tickets() {
		if t.TicketID == ticketID {
			tk = &t
			break
		}
	}
	if tk == nil {
		return Trip{}, fmt.Errorf("ticket %s: %w", ticketID, ErrNotFound)
	}
	store, ok := s.tickets.Store(tk.StoreID)
	if !ok {
		return Trip{}, fmt.Errorf("store %s: %w", tk.StoreID, ErrNotFound)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.engineers[engineerID]; !ok {
		return Trip{}, fmt.Errorf("engineer %s: %w", engineerID, ErrNotFound)
	}
	for _, t := range s.trips {
		if t.TicketID == ticketID {
			return *t, fmt.Errorf("ticket already dispatched as %s: %w", t.TripID, ErrInvalidState)
		}
	}
	now := time.Now().UTC()
	city := strings.ToUpper(strings.ReplaceAll(store.City, " ", ""))
	t := &Trip{
		TripID: s.nextID("TRIP", fmt.Sprintf("%d-%s", now.Year(), city)), TicketID: tk.TicketID, IssueID: tk.IssueID,
		StoreID: store.StoreID, StoreName: store.Name, City: store.City, Country: store.Country,
		StoreLat: store.Latitude, StoreLng: store.Longitude, ControllerID: tk.ControllerID,
		Code: string(tk.Code), RequiredParts: tk.RequiredParts, EngineerID: engineerID,
		Status: TripAssigned, CreatedAt: now, Punches: []Punch{},
	}
	s.trips[t.TripID] = t
	s.tripOrder = append(s.tripOrder, t.TripID)
	s.saveTripLocked(t)
	s.log.Info("trip dispatched", "trip", t.TripID, "ticket", ticketID, "engineer", engineerID)
	return *t, nil
}

// ---- Timesheets -----------------------------------------------------------

type PunchRequest struct {
	EngineerID   string   `json:"engineer_id"`
	TripID       string   `json:"project_trip_id"`
	StoreID      string   `json:"store_id"`
	Action       string   `json:"action"`
	Timestamp    int64    `json:"timestamp"` // client clock, audit only
	GeoLat       *float64 `json:"geo_lat"`
	GeoLng       *float64 `json:"geo_lng"`
	GeoAccuracyM float64  `json:"geo_accuracy_m"`
}

// Punch records CHECK_IN / CHECK_OUT (spec §3.4 #1). Server time is used.
func (s *Service) Punch(req PunchRequest) (Trip, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.engineerTripLocked(req.TripID, req.EngineerID)
	if err != nil {
		return Trip{}, err
	}
	if req.StoreID != "" && req.StoreID != t.StoreID {
		return Trip{}, fmt.Errorf("store_id does not match trip: %w", ErrBadRequest)
	}
	if t.Status == TripSubmitted || t.Status == TripApproved {
		return Trip{}, fmt.Errorf("trip already %s: %w", t.Status, ErrInvalidState)
	}
	switch req.Action {
	case ActionCheckIn:
		if t.Status == TripOnSite {
			return Trip{}, fmt.Errorf("already checked in: %w", ErrInvalidState)
		}
	case ActionCheckOut:
		if t.Status != TripOnSite {
			return Trip{}, fmt.Errorf("not checked in: %w", ErrInvalidState)
		}
	default:
		return Trip{}, fmt.Errorf("action must be CHECK_IN or CHECK_OUT: %w", ErrBadRequest)
	}

	p := Punch{Action: req.Action, At: time.Now().UTC(), ClientTimestamp: req.Timestamp}
	if req.GeoLat != nil && req.GeoLng != nil {
		p.Geo = &GeoPoint{Lat: *req.GeoLat, Lng: *req.GeoLng, AccuracyM: req.GeoAccuracyM}
		d := round2(haversineKm(*req.GeoLat, *req.GeoLng, t.StoreLat, t.StoreLng))
		p.DistanceKm = &d
	}
	t.Punches = append(t.Punches, p)
	t.Status = statusFromPunches(t.Punches)
	s.saveTripLocked(t)
	s.log.Info("punch", "trip", t.TripID, "action", p.Action, "distance_km", p.DistanceKm)

	if p.Action == ActionCheckOut && t.ReminderSentAt == nil {
		s.scheduleReminderLocked(t.TripID, s.cfg.ReminderDelay)
	}
	return *t, nil
}

// Submit closes the trip for the engineer and sends the receipt summary mail
// (spec §3.2 node 4).
func (s *Service) Submit(tripID, engineerID string) (Trip, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.engineerTripLocked(tripID, engineerID)
	if err != nil {
		return Trip{}, err
	}
	if t.Status != TripWorkDone {
		return Trip{}, fmt.Errorf("trip is %s, check out first: %w", t.Status, ErrInvalidState)
	}
	now := time.Now().UTC()
	t.Status, t.SubmittedAt = TripSubmitted, &now
	s.saveTripLocked(t)
	tot := totals(s.tripExpensesLocked(t.TripID))
	eng := s.engineers[t.EngineerID]
	s.sendLocked(Email{Kind: "RECEIPTS_RECEIVED", TripID: t.TripID, To: eng.Email,
		Subject: fmt.Sprintf("[%s] Expense claim received", t.TripID),
		Body: fmt.Sprintf("Hi %s,\n\nReceived %d receipts (€%.2f total) for your trip to %s. "+
			"Your timesheet (%.2f h on site) and expense claim have been logged for approval.\n\n— Rocateq ERP",
			firstName(eng.Name), tot.ReceiptCount, tot.TotalEUR, t.City, mustHours(t.Punches))})
	return *t, nil
}

// Approve is the back-office decision. Receipts still under review block it.
func (s *Service) Approve(tripID, approver string) (Trip, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.trips[tripID]
	if t == nil {
		return Trip{}, ErrNotFound
	}
	if t.Status != TripSubmitted {
		return Trip{}, fmt.Errorf("trip is %s, not submitted: %w", t.Status, ErrInvalidState)
	}
	if n := totals(s.tripExpensesLocked(tripID)).ReceiptsPendingReview; n > 0 {
		return Trip{}, fmt.Errorf("%d receipts still need review: %w", n, ErrInvalidState)
	}
	if approver == "" {
		approver = "Operations"
	}
	now := time.Now().UTC()
	t.Status, t.ApprovedAt, t.ApprovedBy = TripApproved, &now, approver
	s.saveTripLocked(t)
	eng := s.engineers[t.EngineerID]
	s.sendLocked(Email{Kind: "TRIP_APPROVED", TripID: t.TripID, To: eng.Email,
		Subject: fmt.Sprintf("[%s] Approved", t.TripID),
		Body:    fmt.Sprintf("Hi %s,\n\nYour trip to %s has been approved by %s.\n\n— Rocateq ERP", firstName(eng.Name), t.City, approver)})
	return *t, nil
}

func (s *Service) engineerTripLocked(tripID, engineerID string) (*Trip, error) {
	t := s.trips[tripID]
	if t == nil {
		return nil, fmt.Errorf("trip %s: %w", tripID, ErrNotFound)
	}
	if engineerID != "" && t.EngineerID != engineerID {
		return nil, fmt.Errorf("trip %s is not assigned to %s: %w", tripID, engineerID, ErrBadRequest)
	}
	return t, nil
}

// ---- Expenses -------------------------------------------------------------

var allowedImageTypes = map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "image/gif": ".gif"}

// AddExpense stores a receipt photo and starts the LLM extraction in the background.
func (s *Service) AddExpense(tripID, engineerID string, image []byte, sourceName string) (Expense, error) {
	if int64(len(image)) > s.cfg.MaxUploadBytes {
		return Expense{}, fmt.Errorf("image larger than %d MB: %w", s.cfg.MaxUploadBytes>>20, ErrBadRequest)
	}
	mime := http.DetectContentType(image) // never trust the client's content type
	ext, ok := allowedImageTypes[mime]
	if !ok {
		return Expense{}, fmt.Errorf("unsupported image type %s: %w", mime, ErrBadRequest)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.engineerTripLocked(tripID, engineerID)
	if err != nil {
		return Expense{}, err
	}
	if t.Status == TripApproved {
		return Expense{}, fmt.Errorf("trip already approved: %w", ErrInvalidState)
	}
	now := time.Now().UTC()
	e := &Expense{
		ExpenseID: s.nextID("EXP", now.Format("20060102")), TripID: tripID, EngineerID: t.EngineerID,
		ImageMime: mime, SourceName: filepath.Base(sourceName), UploadedAt: now, Status: ExpenseAnalyzing,
	}
	e.ImageFile = e.ExpenseID + ext
	if err := os.WriteFile(s.imagePath(e.ImageFile), image, 0o644); err != nil {
		return Expense{}, err
	}
	s.expenses[e.ExpenseID] = e
	s.expOrder = append(s.expOrder, e.ExpenseID)
	s.saveExpenseLocked(e)
	s.startExtraction(e.ExpenseID)
	return *e, nil
}

func (s *Service) imagePath(file string) string {
	return filepath.Join(s.cfg.UploadDir, "receipts", filepath.Base(file))
}

// ImagePath returns the stored photo of an expense.
func (s *Service) ImagePath(expenseID string) (path, mime string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.expenses[expenseID]
	if e == nil {
		return "", "", ErrNotFound
	}
	return s.imagePath(e.ImageFile), e.ImageMime, nil
}

func (s *Service) startExtraction(expenseID string) {
	if s.closed {
		return
	}
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		s.extract(expenseID)
	}()
}

// extract sends the stored image to the vision model. Nothing but the image
// bytes go to the model: the result is whatever the model reads.
func (s *Service) extract(expenseID string) {
	s.mu.Lock()
	e := s.expenses[expenseID]
	if e == nil {
		s.mu.Unlock()
		return
	}
	path, mime := s.imagePath(e.ImageFile), e.ImageMime
	s.mu.Unlock()

	var (
		r   *llm.Receipt
		err error
	)
	t0 := time.Now()
	img, err := os.ReadFile(path)
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), s.cfg.ExtractTimeout)
		r, err = s.extractor.ExtractReceipt(ctx, img, mime)
		cancel()
	}
	latency := time.Since(t0)

	s.mu.Lock()
	defer s.mu.Unlock()
	e = s.expenses[expenseID]
	if e == nil || e.Status != ExpenseAnalyzing {
		return // edited or retried meanwhile
	}
	e.Model, e.LatencyMs = s.extractor.Name(), latency.Milliseconds()
	if err != nil {
		e.Status, e.Error = ExpenseFailed, err.Error()
		s.log.Warn("receipt extraction failed", "expense", expenseID, "err", err)
	} else {
		cp := *r
		e.Extracted, e.Final, e.Status, e.Error = r, &cp, reviewStatus(r), ""
		s.log.Info("receipt extracted", "expense", expenseID, "merchant", r.MerchantName, "total", r.TotalAmount,
			"category", r.ExpenseCategory, "confidence", r.ConfidenceScore, "status", e.Status, "dur", latency.Round(time.Millisecond))
	}
	s.saveExpenseLocked(e)
}

// ConfirmExpense records the engineer's check. A nil receipt confirms the
// extracted values as they are; otherwise the given values replace them.
func (s *Service) ConfirmExpense(expenseID, engineerID string, edited *llm.Receipt) (Expense, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.expenses[expenseID]
	if e == nil {
		return Expense{}, ErrNotFound
	}
	if engineerID != "" && e.EngineerID != engineerID {
		return Expense{}, fmt.Errorf("expense belongs to another engineer: %w", ErrBadRequest)
	}
	if t := s.trips[e.TripID]; t != nil && t.Status == TripApproved {
		return Expense{}, fmt.Errorf("trip already approved: %w", ErrInvalidState)
	}
	if e.Status == ExpenseAnalyzing {
		return Expense{}, fmt.Errorf("still analyzing: %w", ErrInvalidState)
	}
	final := e.Final
	if edited != nil {
		final = edited
	}
	if final == nil || final.TotalAmount <= 0 || final.TransactionDate == "" {
		return Expense{}, fmt.Errorf("total amount and date are required: %w", ErrBadRequest)
	}
	cp := *final
	cp.ExpenseCategory = strings.ToUpper(cp.ExpenseCategory)
	if cp.Currency == "" {
		cp.Currency = "EUR"
	}
	e.Final, e.Status = &cp, ExpenseConfirmed
	e.EditedByEngineer = e.Extracted == nil || !sameAmounts(e.Extracted, &cp)
	s.saveExpenseLocked(e)
	return *e, nil
}

func sameAmounts(a, b *llm.Receipt) bool {
	return a.TotalAmount == b.TotalAmount && a.VATAmount == b.VATAmount && a.TransactionDate == b.TransactionDate &&
		a.ExpenseCategory == b.ExpenseCategory && a.MerchantName == b.MerchantName
}

// RetryExpense re-runs the LLM on the stored photo.
func (s *Service) RetryExpense(expenseID string) (Expense, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.expenses[expenseID]
	if e == nil {
		return Expense{}, ErrNotFound
	}
	if e.Status == ExpenseAnalyzing {
		return *e, nil
	}
	if t := s.trips[e.TripID]; t != nil && t.Status == TripApproved {
		return Expense{}, fmt.Errorf("trip already approved: %w", ErrInvalidState)
	}
	e.Status, e.Error = ExpenseAnalyzing, ""
	s.saveExpenseLocked(e)
	s.startExtraction(e.ExpenseID)
	return *e, nil
}

// ---- Read models ----------------------------------------------------------

func (s *Service) Trips(engineerID string) []TripDetail {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []TripDetail
	for i := len(s.tripOrder) - 1; i >= 0; i-- {
		t := s.trips[s.tripOrder[i]]
		if engineerID == "" || t.EngineerID == engineerID {
			out = append(out, s.detailLocked(t))
		}
	}
	return out
}

func (s *Service) Trip(tripID string) (TripDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.trips[tripID]
	if t == nil {
		return TripDetail{}, ErrNotFound
	}
	return s.detailLocked(t), nil
}

func (s *Service) detailLocked(t *Trip) TripDetail {
	exp := s.tripExpensesLocked(t.TripID)
	cp := *t
	cp.Punches = append([]Punch{}, t.Punches...)
	return TripDetail{Trip: cp, Engineer: s.engineers[t.EngineerID], Summary: s.summaryLocked(t, exp), Expenses: exp}
}

func (s *Service) summaryLocked(t *Trip, exp []Expense) TripSummary {
	hours, onSite := OnsiteHours(t.Punches, time.Now())
	sum := TripSummary{
		TripID: t.TripID, EngineerName: s.engineers[t.EngineerID].Name,
		Destination: t.City + ", " + t.Country, TotalOnsiteHours: hours, OnSiteNow: onSite,
		Expenses: totals(exp), ReceiptEnvelope: receiptEnvelope(exp), TripStatus: t.Status,
	}
	for _, p := range t.Punches {
		if p.DistanceKm != nil && *p.DistanceKm > GeoWarnKm {
			sum.GeoWarnings++
		}
	}
	switch {
	case t.Status == TripApproved:
		sum.Status = "APPROVED"
	case t.Status != TripSubmitted:
		sum.Status = "IN_PROGRESS"
	case sum.Expenses.ReceiptsPendingReview > 0:
		sum.Status = "AWAITING_RECEIPT_REVIEW"
	default:
		sum.Status = "READY_FOR_APPROVAL"
	}
	return sum
}

func (s *Service) tripExpensesLocked(tripID string) []Expense {
	out := []Expense{}
	for _, id := range s.expOrder {
		if e := s.expenses[id]; e.TripID == tripID {
			out = append(out, *e)
		}
	}
	return out
}

func (s *Service) Outbox() []Email {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Email{}, s.emails...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].SentAt.After(out[j].SentAt) })
	return out
}

// AllExpenses is used by the ops dashboard.
func (s *Service) AllExpenses() []Expense {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Expense, 0, len(s.expOrder))
	for _, id := range s.expOrder {
		out = append(out, *s.expenses[id])
	}
	return out
}

// ---- Reminder mail (stand-in for the n8n cron of spec §3.2 node 1) --------

func (s *Service) scheduleReminderLocked(tripID string, delay time.Duration) {
	if s.closed || s.timers[tripID] != nil {
		return
	}
	s.timers[tripID] = time.AfterFunc(delay, func() { s.sendReminder(tripID) })
}

func (s *Service) sendReminder(tripID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.timers, tripID)
	t := s.trips[tripID]
	// Only remind while the engineer still has to hand in receipts.
	if t == nil || t.ReminderSentAt != nil || t.Status != TripWorkDone {
		return
	}
	eng := s.engineers[t.EngineerID]
	n := len(s.tripExpensesLocked(tripID))
	link := fmt.Sprintf("%s/engineer/?engineer_id=%s&trip_id=%s", strings.TrimRight(s.cfg.PublicBaseURL, "/"), eng.EngineerID, t.TripID)
	s.sendLocked(Email{Kind: "RECEIPT_REMINDER", TripID: tripID, To: eng.Email,
		Subject: fmt.Sprintf("[%s] Expense reminder — your trip to %s", t.TripID, t.City),
		Body: fmt.Sprintf("Hi %s,\n\nYour work at %s is done. Please snap photos of your travel receipts "+
			"(hotel, meals, train/flight tickets, taxi) — %d uploaded so far. The amounts are read automatically.\n\n"+
			"Upload here: %s\n\n— Rocateq ERP", firstName(eng.Name), t.StoreName, n, link)})
	now := time.Now().UTC()
	t.ReminderSentAt = &now
	s.saveTripLocked(t)
}

// sendLocked delivers through the outbox (demo). Production: SMTP / n8n.
func (s *Service) sendLocked(m Email) {
	m.SentAt = time.Now().UTC()
	m.EmailID = s.nextID("MAIL", m.SentAt.Format("20060102"))
	s.emails = append(s.emails, m)
	if err := s.outboxLog.Append(m); err != nil {
		s.log.Error("persist email", "err", err)
	}
	s.log.Info("email sent (outbox)", "kind", m.Kind, "to", m.To, "trip", m.TripID)
}

func (s *Service) saveTripLocked(t *Trip) {
	if err := s.tripLog.Append(*t); err != nil {
		s.log.Error("persist trip", "err", err)
	}
}

func (s *Service) saveExpenseLocked(e *Expense) {
	if err := s.expenseLog.Append(*e); err != nil {
		s.log.Error("persist expense", "err", err)
	}
}

// Close stops timers and waits for in-flight extractions.
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	for _, t := range s.timers {
		t.Stop()
	}
	s.mu.Unlock()
	s.background.Wait()
	s.tripLog.Close()
	s.expenseLog.Close()
	s.outboxLog.Close()
}

func firstName(n string) string {
	f, _, _ := strings.Cut(n, " ")
	return f
}

func mustHours(p []Punch) float64 {
	h, _ := OnsiteHours(p, time.Now())
	return h
}

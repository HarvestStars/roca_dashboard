package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/harveststars/roca_dashboard/internal/erp"
	"github.com/harveststars/roca_dashboard/internal/llm"
	"github.com/harveststars/roca_dashboard/internal/model"
)

const maxUploadBytes = 10 << 20

func decode(r *http.Request, v any) error {
	if r.Body == nil {
		return nil
	}
	err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return fmt.Errorf("invalid JSON body: %w", erp.ErrBadRequest)
	}
	return nil
}

func (s *Server) listEngineers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, s.erp.Engineers())
}

func (s *Server) dispatchQueue(w http.ResponseWriter, _ *http.Request) {
	q := s.erp.DispatchQueue()
	if q == nil {
		q = []model.Ticket{}
	}
	writeJSON(w, 200, q)
}

// POST /api/v1/trips {ticket_id, engineer_id} — dispatch a ticket.
func (s *Server) postDispatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TicketID   string `json:"ticket_id"`
		EngineerID string `json:"engineer_id"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	t, err := s.erp.Dispatch(body.TicketID, body.EngineerID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, t)
}

func (s *Server) listTrips(w http.ResponseWriter, r *http.Request) {
	trips := s.erp.Trips(r.URL.Query().Get("engineer_id"))
	if trips == nil {
		trips = []erp.TripDetail{}
	}
	writeJSON(w, 200, trips)
}

func (s *Server) getTrip(w http.ResponseWriter, r *http.Request) {
	d, err := s.erp.Trip(r.PathValue("trip_id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, d)
}

// GET /api/v1/trips/{trip_id}/summary — spec §3.4 #2.
func (s *Server) getTripSummary(w http.ResponseWriter, r *http.Request) {
	d, err := s.erp.Trip(r.PathValue("trip_id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, d.Summary)
}

func (s *Server) postSubmit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EngineerID string `json:"engineer_id"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	t, err := s.erp.Submit(r.PathValue("trip_id"), body.EngineerID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, t)
}

func (s *Server) postApprove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Approver string `json:"approver"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	t, err := s.erp.Approve(r.PathValue("trip_id"), body.Approver)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, t)
}

// POST /api/v1/engineers/timesheets/punch — spec §3.4 #1.
func (s *Server) postPunch(w http.ResponseWriter, r *http.Request) {
	var req erp.PunchRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	t, err := s.erp.Punch(req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, t)
}

// POST /api/v1/trips/{trip_id}/expenses — multipart: file, engineer_id.
func (s *Server) postExpense(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+1<<20)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		writeErr(w, fmt.Errorf("upload too large or not multipart: %w", erp.ErrBadRequest))
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, fmt.Errorf("missing file field: %w", erp.ErrBadRequest))
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxUploadBytes+1))
	if err != nil {
		writeErr(w, err)
		return
	}
	e, err := s.erp.AddExpense(r.PathValue("trip_id"), r.FormValue("engineer_id"), data, hdr.Filename)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 202, e)
}

// POST /api/v1/trips/{trip_id}/expenses/sample {name, engineer_id}
// Demo helper for laptops without a camera: uploads one of the generated
// sample receipt images through exactly the same pipeline as a photo.
func (s *Server) postSampleExpense(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name       string `json:"name"`
		EngineerID string `json:"engineer_id"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	data, err := s.readSample(body.Name)
	if err != nil {
		writeErr(w, err)
		return
	}
	e, err := s.erp.AddExpense(r.PathValue("trip_id"), body.EngineerID, data, body.Name)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 202, e)
}

func (s *Server) sampleNames() []string {
	var names []string
	entries, _ := os.ReadDir(s.samples)
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !e.IsDir() && (ext == ".jpg" || ext == ".jpeg" || ext == ".png") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

func (s *Server) readSample(name string) ([]byte, error) {
	for _, n := range s.sampleNames() { // whitelist: no path tricks
		if n == name {
			return os.ReadFile(filepath.Join(s.samples, n))
		}
	}
	return nil, fmt.Errorf("sample %q: %w", name, erp.ErrNotFound)
}

func (s *Server) listSampleReceipts(w http.ResponseWriter, _ *http.Request) {
	names := s.sampleNames()
	if names == nil {
		names = []string{}
	}
	writeJSON(w, 200, names)
}

func (s *Server) getSampleReceipt(w http.ResponseWriter, r *http.Request) {
	data, err := s.readSample(r.PathValue("name"))
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", http.DetectContentType(data))
	w.Header().Set("Cache-Control", "max-age=300")
	w.Write(data)
}

func (s *Server) getExpenseImage(w http.ResponseWriter, r *http.Request) {
	path, mime, err := s.erp.ImagePath(r.PathValue("expense_id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(w, r, path)
}

// POST /api/v1/expenses/{id}/confirm {engineer_id, receipt?}
func (s *Server) postConfirmExpense(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EngineerID string       `json:"engineer_id"`
		Receipt    *llm.Receipt `json:"receipt"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	e, err := s.erp.ConfirmExpense(r.PathValue("expense_id"), body.EngineerID, body.Receipt)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, e)
}

func (s *Server) postRetryExpense(w http.ResponseWriter, r *http.Request) {
	e, err := s.erp.RetryExpense(r.PathValue("expense_id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 202, e)
}

func (s *Server) getOutbox(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, s.erp.Outbox())
}

// ---- Grafana feeds --------------------------------------------------------

func (s *Server) opsERPSummary(w http.ResponseWriter, _ *http.Request) {
	var out struct {
		TripsActive     int     `json:"trips_active"`
		TripsAwaiting   int     `json:"trips_awaiting_approval"`
		TripsApproved   int     `json:"trips_approved"`
		OnsiteHours     float64 `json:"onsite_hours_total"`
		ExpensesEUR     float64 `json:"expenses_total_eur"`
		Receipts        int     `json:"receipts_total"`
		ReceiptsPending int     `json:"receipts_pending_review"`
		ReceiptsAuto    int     `json:"receipts_auto_accepted"`
	}
	for _, d := range s.erp.Trips("") {
		switch d.Trip.Status {
		case erp.TripApproved:
			out.TripsApproved++
		case erp.TripSubmitted:
			out.TripsAwaiting++
		default:
			out.TripsActive++
		}
		out.OnsiteHours += d.Summary.TotalOnsiteHours
		out.ExpensesEUR += d.Summary.Expenses.TotalEUR
		out.Receipts += d.Summary.Expenses.ReceiptCount
		out.ReceiptsPending += d.Summary.Expenses.ReceiptsPendingReview
	}
	for _, e := range s.erp.AllExpenses() {
		if e.AutoAccepted() {
			out.ReceiptsAuto++
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) opsERPTrips(w http.ResponseWriter, _ *http.Request) {
	type row struct {
		TripID      string  `json:"trip_id"`
		Status      string  `json:"status"`
		Engineer    string  `json:"engineer"`
		Store       string  `json:"store"`
		City        string  `json:"city"`
		TicketID    string  `json:"ticket_id"`
		Code        string  `json:"code"`
		OnsiteHours float64 `json:"onsite_hours"`
		ExpensesEUR float64 `json:"expenses_eur"`
		Receipts    int     `json:"receipts"`
		Pending     int     `json:"receipts_pending"`
		GeoWarnings int     `json:"geo_warnings"`
		Envelope    float64 `json:"receipt_envelope_hours"`
		CreatedAt   string  `json:"created_at"`
	}
	out := []row{}
	for _, d := range s.erp.Trips("") {
		r := row{TripID: d.Trip.TripID, Status: d.Summary.Status, Engineer: d.Engineer.Name, Store: d.Trip.StoreName,
			City: d.Trip.City, TicketID: d.Trip.TicketID, Code: d.Trip.Code, OnsiteHours: d.Summary.TotalOnsiteHours,
			ExpensesEUR: d.Summary.Expenses.TotalEUR, Receipts: d.Summary.Expenses.ReceiptCount,
			Pending: d.Summary.Expenses.ReceiptsPendingReview, GeoWarnings: d.Summary.GeoWarnings,
			CreatedAt: d.Trip.CreatedAt.Format(time.RFC3339)}
		if env := d.Summary.ReceiptEnvelope; env != nil {
			r.Envelope = env.SpanHours
		}
		out = append(out, r)
	}
	writeJSON(w, 200, out)
}

func (s *Server) opsERPCategories(w http.ResponseWriter, _ *http.Request) {
	sum := map[string]float64{}
	for _, e := range s.erp.AllExpenses() {
		if e.Final != nil {
			sum[e.Final.ExpenseCategory] += e.Final.TotalAmount
		}
	}
	type row struct {
		Category string  `json:"category"`
		EUR      float64 `json:"eur"`
	}
	out := []row{}
	for c, v := range sum {
		out = append(out, row{c, float64(int(v*100+0.5)) / 100})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EUR > out[j].EUR })
	writeJSON(w, 200, out)
}

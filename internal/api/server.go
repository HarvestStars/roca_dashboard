// Package api exposes the REST surface: the store-manager wizard API
// (spec §2.6), the field-engineer / back-office ERP API (spec §3.4) and flat
// JSON feeds for the Grafana operations dashboard.
package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/harveststars/roca_dashboard/internal/diagnostics"
	"github.com/harveststars/roca_dashboard/internal/erp"
	"github.com/harveststars/roca_dashboard/internal/llm"
	"github.com/harveststars/roca_dashboard/internal/storage"
)

type Server struct {
	engine  *diagnostics.Engine
	guides  *llm.GuideService
	tel     storage.TelemetryStore
	erp     *erp.Service
	samples string
	log     *slog.Logger
}

// Deps are the modules the HTTP layer is wired to.
type Deps struct {
	Engine            *diagnostics.Engine
	Guides            *llm.GuideService
	Telemetry         storage.TelemetryStore
	ERP               *erp.Service
	SampleReceiptsDir string
	Web               fs.FS // contains store/, engineer/, backoffice/
	Log               *slog.Logger
}

func New(d Deps) http.Handler {
	s := &Server{engine: d.Engine, guides: d.Guides, tel: d.Telemetry, erp: d.ERP, samples: d.SampleReceiptsDir, log: d.Log}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })

	// Requirement 1 — store manager (B端)
	mux.HandleFunc("GET /api/v1/stores", s.listStores)
	mux.HandleFunc("GET /api/v1/stores/{store_id}/diagnostics/wizard", s.getWizard)
	mux.HandleFunc("POST /api/v1/stores/{store_id}/diagnostics/wizard/verify", s.postVerify)
	mux.HandleFunc("POST /api/v1/stores/{store_id}/tickets/escalate", s.postEscalate)

	// Requirement 2 — field engineers & back office
	mux.HandleFunc("GET /api/v1/engineers", s.listEngineers)
	mux.HandleFunc("GET /api/v1/dispatch/queue", s.dispatchQueue)
	mux.HandleFunc("POST /api/v1/trips", s.postDispatch)
	mux.HandleFunc("GET /api/v1/trips", s.listTrips)
	mux.HandleFunc("GET /api/v1/trips/{trip_id}", s.getTrip)
	mux.HandleFunc("GET /api/v1/trips/{trip_id}/summary", s.getTripSummary)
	mux.HandleFunc("POST /api/v1/trips/{trip_id}/submit", s.postSubmit)
	mux.HandleFunc("POST /api/v1/trips/{trip_id}/approve", s.postApprove)
	mux.HandleFunc("POST /api/v1/engineers/timesheets/punch", s.postPunch)
	mux.HandleFunc("POST /api/v1/trips/{trip_id}/expenses", s.postExpense)
	mux.HandleFunc("POST /api/v1/trips/{trip_id}/expenses/sample", s.postSampleExpense)
	mux.HandleFunc("GET /api/v1/expenses/{expense_id}/image", s.getExpenseImage)
	mux.HandleFunc("POST /api/v1/expenses/{expense_id}/confirm", s.postConfirmExpense)
	mux.HandleFunc("POST /api/v1/expenses/{expense_id}/retry", s.postRetryExpense)
	mux.HandleFunc("GET /api/v1/erp/outbox", s.getOutbox)
	mux.HandleFunc("GET /api/v1/erp/sample-receipts", s.listSampleReceipts)
	mux.HandleFunc("GET /api/v1/erp/sample-receipts/{name}", s.getSampleReceipt)

	// Operations dashboard feeds (Grafana Infinity datasource)
	mux.HandleFunc("GET /api/v1/ops/summary", s.opsSummary)
	mux.HandleFunc("GET /api/v1/ops/stores", s.opsStores)
	mux.HandleFunc("GET /api/v1/ops/issues", s.opsIssues)
	mux.HandleFunc("GET /api/v1/ops/tickets", s.opsTickets)
	mux.HandleFunc("GET /api/v1/ops/root-causes", s.opsRootCauses)
	mux.HandleFunc("GET /api/v1/ops/telemetry", s.opsTelemetry)
	mux.HandleFunc("GET /api/v1/ops/erp/summary", s.opsERPSummary)
	mux.HandleFunc("GET /api/v1/ops/erp/trips", s.opsERPTrips)
	mux.HandleFunc("GET /api/v1/ops/erp/expense-categories", s.opsERPCategories)

	// Static UIs
	for _, app := range []string{"store", "engineer", "backoffice"} {
		sub, _ := fs.Sub(d.Web, app)
		mux.Handle("GET /"+app+"/", http.StripPrefix("/"+app+"/", http.FileServerFS(sub)))
	}
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/store/", http.StatusFound) })

	return logRequests(d.Log, cors(mux))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	switch {
	case errors.Is(err, diagnostics.ErrNotFound), errors.Is(err, erp.ErrNotFound):
		code = http.StatusNotFound
	case errors.Is(err, erp.ErrBadRequest):
		code = http.StatusBadRequest
	case errors.Is(err, erp.ErrInvalidState):
		code = http.StatusConflict
	}
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (r *statusRecorder) WriteHeader(c int) { r.code = c; r.ResponseWriter.WriteHeader(c) }

func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, code: 200}
		next.ServeHTTP(rec, r)
		// Polling GETs would flood the console; log writes and errors only.
		if r.Method != http.MethodGet || rec.code >= 400 {
			log.Info("http", "method", r.Method, "path", r.URL.Path, "status", rec.code, "dur", time.Since(start).Round(time.Millisecond))
		}
	})
}

// Package api exposes the REST surface: the store-manager wizard API
// (spec §2.6) and flat JSON feeds for the Grafana operations dashboard.
package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/harveststars/roca_dashboard/internal/diagnostics"
	"github.com/harveststars/roca_dashboard/internal/llm"
	"github.com/harveststars/roca_dashboard/internal/storage"
)

type Server struct {
	engine *diagnostics.Engine
	guides *llm.GuideService
	tel    storage.TelemetryStore
	log    *slog.Logger
}

func New(engine *diagnostics.Engine, guides *llm.GuideService, tel storage.TelemetryStore, web fs.FS, log *slog.Logger) http.Handler {
	s := &Server{engine: engine, guides: guides, tel: tel, log: log}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })

	// Store manager (B端)
	mux.HandleFunc("GET /api/v1/stores", s.listStores)
	mux.HandleFunc("GET /api/v1/stores/{store_id}/diagnostics/wizard", s.getWizard)
	mux.HandleFunc("POST /api/v1/stores/{store_id}/diagnostics/wizard/verify", s.postVerify)
	mux.HandleFunc("POST /api/v1/stores/{store_id}/tickets/escalate", s.postEscalate)

	// Operations dashboard feeds (Grafana Infinity datasource)
	mux.HandleFunc("GET /api/v1/ops/summary", s.opsSummary)
	mux.HandleFunc("GET /api/v1/ops/stores", s.opsStores)
	mux.HandleFunc("GET /api/v1/ops/issues", s.opsIssues)
	mux.HandleFunc("GET /api/v1/ops/tickets", s.opsTickets)
	mux.HandleFunc("GET /api/v1/ops/root-causes", s.opsRootCauses)
	mux.HandleFunc("GET /api/v1/ops/telemetry", s.opsTelemetry)

	// Static store UI
	mux.Handle("GET /store/", http.StripPrefix("/store/", http.FileServerFS(web)))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/store/", http.StatusFound) })

	return logRequests(log, cors(mux))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	if errors.Is(err, diagnostics.ErrNotFound) {
		code = http.StatusNotFound
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

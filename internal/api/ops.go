package api

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/harveststars/roca_dashboard/internal/model"
)

// Ops feeds return flat JSON arrays/objects so the Grafana Infinity
// datasource can map them to tables without transformations.

func (s *Server) opsSummary(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, s.engine.Summary())
}

func (s *Server) opsStores(w http.ResponseWriter, _ *http.Request) {
	type row struct {
		StoreID     string  `json:"store_id"`
		Name        string  `json:"name"`
		Chain       string  `json:"chain"`
		City        string  `json:"city"`
		Country     string  `json:"country"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
		Status      string  `json:"status"`
		StatusRank  int     `json:"status_rank"`
		Controllers int     `json:"controllers"`
		IssueCode   string  `json:"issue_code"`
		IssueSince  string  `json:"issue_since"`
		LastSeen    string  `json:"last_seen"`
	}
	out := []row{}
	for _, v := range s.engine.Stores() {
		r := row{StoreID: v.StoreID, Name: v.Name, Chain: v.Chain, City: v.City, Country: v.Country,
			Latitude: v.Latitude, Longitude: v.Longitude, Status: string(v.SystemStatus),
			StatusRank: v.SystemStatus.Rank(), Controllers: len(v.Controllers)}
		if v.ActiveIssue != nil {
			r.IssueCode = string(v.ActiveIssue.Finding.Code)
			r.IssueSince = v.ActiveIssue.OpenedAt.Format(time.RFC3339)
		}
		var last time.Time
		for _, c := range v.Controllers {
			if c.LastSeen != nil && c.LastSeen.After(last) {
				last = *c.LastSeen
			}
		}
		if !last.IsZero() {
			r.LastSeen = last.Format(time.RFC3339)
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StatusRank > out[j].StatusRank })
	writeJSON(w, 200, out)
}

func (s *Server) opsIssues(w http.ResponseWriter, r *http.Request) {
	type row struct {
		IssueID        string  `json:"issue_id"`
		StoreID        string  `json:"store_id"`
		ControllerID   string  `json:"controller_id"`
		Code           string  `json:"code"`
		Severity       string  `json:"severity"`
		SelfFixable    bool    `json:"self_fixable"`
		State          string  `json:"state"`
		Resolution     string  `json:"resolution"`
		OpenedAt       string  `json:"opened_at"`
		ResolvedAt     string  `json:"resolved_at"`
		DurationMin    float64 `json:"duration_min"`
		VerifyAttempts int     `json:"verify_attempts"`
		TicketID       string  `json:"ticket_id"`
		Target         string  `json:"target"`
	}
	openOnly := r.URL.Query().Get("state") == "open"
	out := []row{}
	now := time.Now()
	for _, iss := range s.engine.Issues(openOnly) {
		end := now
		rr := row{IssueID: iss.IssueID, StoreID: iss.StoreID, ControllerID: iss.ControllerID, Code: string(iss.Finding.Code),
			Severity: string(iss.Finding.Severity), SelfFixable: iss.Finding.IsSelfFixable, State: string(iss.State),
			Resolution: string(iss.Resolution), OpenedAt: iss.OpenedAt.Format(time.RFC3339),
			VerifyAttempts: iss.VerifyAttempts, TicketID: iss.TicketID, Target: iss.Finding.TargetModule}
		if iss.ResolvedAt != nil {
			end = *iss.ResolvedAt
			rr.ResolvedAt = iss.ResolvedAt.Format(time.RFC3339)
		}
		rr.DurationMin = float64(int(end.Sub(iss.OpenedAt).Minutes()*10)) / 10
		out = append(out, rr)
	}
	writeJSON(w, 200, out)
}

func (s *Server) opsTickets(w http.ResponseWriter, _ *http.Request) {
	type row struct {
		TicketID       string `json:"ticket_id"`
		StoreID        string `json:"store_id"`
		ControllerID   string `json:"controller_id"`
		IssueID        string `json:"issue_id"`
		Code           string `json:"code"`
		Source         string `json:"source"`
		Status         string `json:"status"`
		CreatedAt      string `json:"created_at"`
		RequiredParts  string `json:"required_parts"`
		WizardAttempts int    `json:"wizard_attempts"`
		Note           string `json:"note"`
	}
	out := []row{}
	for _, t := range s.engine.Tickets() {
		parts := ""
		for i, p := range t.RequiredParts {
			if i > 0 {
				parts += ", "
			}
			parts += p
		}
		out = append(out, row{t.TicketID, t.StoreID, t.ControllerID, t.IssueID, string(t.Code), string(t.Source),
			t.Status, t.CreatedAt.Format(time.RFC3339), parts, t.WizardAttempts, t.Note})
	}
	writeJSON(w, 200, out)
}

func (s *Server) opsRootCauses(w http.ResponseWriter, _ *http.Request) {
	counts := map[string]int{}
	for _, iss := range s.engine.Issues(false) {
		if iss.Resolution != model.ResolutionSuperseded {
			counts[string(iss.Finding.Code)]++
		}
	}
	type row struct {
		Code  string `json:"code"`
		Count int    `json:"count"`
	}
	out := []row{}
	for c, n := range counts {
		out = append(out, row{c, n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	writeJSON(w, 200, out)
}

// GET /api/v1/ops/telemetry?metric=voltage_mv&from=<ms>&to=<ms>&step_ms=<ms>
//
// Long format rows {time, series, value}; Grafana splits them per series with
// the "partition by values" transformation. Points are down-sampled to the
// last value per series per step bucket.
func (s *Server) opsTelemetry(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	now := time.Now()
	from := msParam(q.Get("from"), now.Add(-time.Hour))
	to := msParam(q.Get("to"), now)
	step, _ := strconv.ParseInt(q.Get("step_ms"), 10, 64)
	step = max(step, 1000)
	metric := q.Get("metric")

	type point struct {
		Time   int64   `json:"time"`
		Series string  `json:"series"`
		Value  float64 `json:"value"`
	}
	type bucketKey struct {
		series string
		bucket int64
	}
	last := map[bucketKey]point{}
	for _, rec := range s.tel.Query(from, to) {
		t := rec.Telemetry
		base := t.StoreID + "/" + t.ControllerID
		ts := rec.ReceivedAt.UnixMilli()
		add := func(series string, v float64) {
			last[bucketKey{series, ts / step}] = point{ts, series, v}
		}
		switch metric {
		case "voltage_mv":
			add(base, float64(t.Power.VoltageMv))
		case "current_ma":
			add(base, float64(t.Power.CurrentMa))
		case "temp_celsius":
			add(base, t.Ambient.TempCelsius)
		case "loop_impedance", "antenna_impedance":
			want := model.PeripheralLoop
			if metric == "antenna_impedance" {
				want = model.PeripheralAntenna
			}
			for name, p := range t.Peripherals {
				if p.Kind(name) == want && p.ImpedanceOhms != nil {
					add(base+"/"+name, float64(*p.ImpedanceOhms))
				}
			}
		}
	}
	out := make([]point, 0, len(last))
	for _, p := range last {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Time != out[j].Time {
			return out[i].Time < out[j].Time
		}
		return out[i].Series < out[j].Series
	})
	writeJSON(w, 200, out)
}

func msParam(s string, def time.Time) time.Time {
	ms, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return def
	}
	return time.UnixMilli(ms)
}

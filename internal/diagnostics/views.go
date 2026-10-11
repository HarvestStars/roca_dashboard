package diagnostics

import (
	"sort"
	"time"

	"github.com/harveststars/roca_dashboard/internal/model"
)

// Read side of the engine: snapshots consumed by the REST API. All methods
// return copies so callers never touch engine state without the lock.

type ControllerView struct {
	ControllerID string          `json:"controller_id"`
	Model        string          `json:"model"`
	Location     string          `json:"location"`
	Status       model.Status    `json:"status"`
	LastSeen     *time.Time      `json:"last_seen,omitempty"`
	Firmware     string          `json:"firmware_version,omitempty"`
	Findings     []model.Finding `json:"findings"`
	OpenIssueID  string          `json:"open_issue_id,omitempty"`
}

type StoreView struct {
	StoreID      string           `json:"store_id"`
	Name         string           `json:"name"`
	Chain        string           `json:"chain"`
	City         string           `json:"city"`
	Country      string           `json:"country"`
	Language     string           `json:"language"`
	Latitude     float64          `json:"latitude"`
	Longitude    float64          `json:"longitude"`
	SystemStatus model.Status     `json:"system_status"`
	Controllers  []ControllerView `json:"controllers"`
	ActiveIssue  *model.Issue     `json:"active_issue"`
	// ActiveController describes where the active issue is (for the guide).
	ActiveController *model.ControllerInfo `json:"-"`
}

func (e *Engine) Store(storeID string) (StoreView, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.stores[storeID]; !ok {
		return StoreView{}, false
	}
	return e.storeViewLocked(storeID), true
}

func (e *Engine) Stores() []StoreView {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]StoreView, 0, len(e.storeOrder))
	for _, id := range e.storeOrder {
		out = append(out, e.storeViewLocked(id))
	}
	return out
}

func (e *Engine) storeViewLocked(storeID string) StoreView {
	s := e.stores[storeID]
	v := StoreView{
		StoreID: s.StoreID, Name: s.Name, Chain: s.Chain, City: s.City, Country: s.Country,
		Language: s.Language, Latitude: s.Latitude, Longitude: s.Longitude,
		SystemStatus: e.storeStatusLocked(storeID),
	}
	for _, c := range s.Controllers {
		st := e.ctrls[key(storeID, c.ControllerID)]
		cv := ControllerView{ControllerID: c.ControllerID, Model: c.Model, Location: c.Location,
			Status: e.ctrlStatusLocked(st), Findings: append([]model.Finding{}, st.findings...), OpenIssueID: st.openIssueID}
		if !st.lastSeen.IsZero() {
			t := st.lastSeen.UTC()
			cv.LastSeen = &t
		}
		if st.last != nil {
			cv.Firmware = st.last.FirmwareVersion
		}
		v.Controllers = append(v.Controllers, cv)
	}
	if iss, info := e.activeIssueLocked(storeID); iss != nil {
		cp := *iss
		v.ActiveIssue, v.ActiveController = &cp, info
	}
	return v
}

func (e *Engine) ctrlStatusLocked(st *ctrlState) model.Status {
	if iss := e.issues[st.openIssueID]; iss != nil {
		switch {
		case iss.Finding.Code == model.RootCauseNetworkOffline:
			return model.StatusOffline
		case iss.State == model.IssueEscalated:
			return model.StatusEscalated
		case iss.Finding.IsSelfFixable:
			return model.StatusActionRequired
		default:
			return model.StatusServiceRequired
		}
	}
	if st.lastSeen.IsZero() {
		return model.StatusUnknown
	}
	return model.StatusHealthy
}

func (e *Engine) storeStatusLocked(storeID string) model.Status {
	worst := model.StatusHealthy
	for _, c := range e.stores[storeID].Controllers {
		if s := e.ctrlStatusLocked(e.ctrls[key(storeID, c.ControllerID)]); s.Rank() > worst.Rank() {
			worst = s
		}
	}
	return worst
}

// activeIssueLocked returns the open issue of the store's worst controller.
func (e *Engine) activeIssueLocked(storeID string) (*model.Issue, *model.ControllerInfo) {
	var best *model.Issue
	var bestInfo *model.ControllerInfo
	bestRank := -1
	for _, c := range e.stores[storeID].Controllers {
		st := e.ctrls[key(storeID, c.ControllerID)]
		iss := e.issues[st.openIssueID]
		if iss == nil {
			continue
		}
		if r := e.ctrlStatusLocked(st).Rank(); r > bestRank {
			info := st.info
			best, bestInfo, bestRank = iss, &info, r
		}
	}
	return best, bestInfo
}

// Issues returns issues newest first; openOnly filters out resolved ones.
func (e *Engine) Issues(openOnly bool) []model.Issue {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []model.Issue
	for _, iss := range e.issues {
		if openOnly && iss.State == model.IssueResolved {
			continue
		}
		out = append(out, *iss)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OpenedAt.After(out[j].OpenedAt) })
	return out
}

// Tickets returns tickets newest first.
func (e *Engine) Tickets() []model.Ticket {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]model.Ticket, 0, len(e.ticketOrder))
	for i := len(e.ticketOrder) - 1; i >= 0; i-- {
		out = append(out, *e.tickets[e.ticketOrder[i]])
	}
	return out
}

type Summary struct {
	StoresTotal             int     `json:"stores_total"`
	ControllersTotal        int     `json:"controllers_total"`
	Healthy                 int     `json:"healthy"`
	ActionRequired          int     `json:"action_required"`
	Escalated               int     `json:"escalated"`
	ServiceRequired         int     `json:"service_required"`
	Offline                 int     `json:"offline"`
	Unknown                 int     `json:"unknown"`
	OpenIssues              int     `json:"open_issues"`
	TicketsOpen             int     `json:"tickets_open"`
	IssuesSelfResolved      int     `json:"issues_self_resolved"`
	IssuesResolvedByService int     `json:"issues_resolved_by_service"`
	SelfResolveRate         float64 `json:"self_resolve_rate"` // 0..1
	TripsAvoided            int     `json:"trips_avoided"`
	EstSavingsEUR           int     `json:"est_savings_eur"`
}

func (e *Engine) Summary() Summary {
	e.mu.Lock()
	defer e.mu.Unlock()
	var s Summary
	s.StoresTotal = len(e.storeOrder)
	for _, id := range e.storeOrder {
		s.ControllersTotal += len(e.stores[id].Controllers)
		switch e.storeStatusLocked(id) {
		case model.StatusHealthy:
			s.Healthy++
		case model.StatusActionRequired:
			s.ActionRequired++
		case model.StatusEscalated:
			s.Escalated++
		case model.StatusServiceRequired:
			s.ServiceRequired++
		case model.StatusOffline:
			s.Offline++
		default:
			s.Unknown++
		}
	}
	for _, iss := range e.issues {
		switch {
		case iss.State != model.IssueResolved:
			s.OpenIssues++
		case iss.Resolution == model.ResolutionSelf:
			s.IssuesSelfResolved++
		case iss.Resolution == model.ResolutionService:
			s.IssuesResolvedByService++
		}
	}
	for _, t := range e.tickets {
		if t.Status == "OPEN" {
			s.TicketsOpen++
		}
	}
	if done := s.IssuesSelfResolved + s.IssuesResolvedByService; done > 0 {
		s.SelfResolveRate = float64(s.IssuesSelfResolved) / float64(done)
	}
	// Every self-resolved issue is a call that, before this platform, ended in a site visit.
	s.TripsAvoided = s.IssuesSelfResolved
	s.EstSavingsEUR = s.TripsAvoided * e.cfg.AvoidedTripCostEUR
	return s
}

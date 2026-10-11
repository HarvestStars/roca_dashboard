package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/harveststars/roca_dashboard/internal/diagnostics"
	"github.com/harveststars/roca_dashboard/internal/llm"
	"github.com/harveststars/roca_dashboard/internal/model"
)

func (s *Server) listStores(w http.ResponseWriter, _ *http.Request) {
	type item struct {
		StoreID  string       `json:"store_id"`
		Name     string       `json:"name"`
		City     string       `json:"city"`
		Language string       `json:"language"`
		Status   model.Status `json:"system_status"`
	}
	var out []item
	for _, v := range s.engine.Stores() {
		out = append(out, item{v.StoreID, v.Name, v.City, v.Language, v.SystemStatus})
	}
	writeJSON(w, 200, out)
}

type wizardResponse struct {
	StoreID      string                       `json:"store_id"`
	StoreName    string                       `json:"store_name"`
	Language     string                       `json:"language"`
	SystemStatus model.Status                 `json:"system_status"`
	Controllers  []diagnostics.ControllerView `json:"controllers"`
	ActiveIssue  *activeIssue                 `json:"active_issue"`
	LLMProvider  string                       `json:"llm_provider"`
}

type activeIssue struct {
	IssueID        string              `json:"issue_id"`
	ControllerID   string              `json:"controller_id"`
	DetectedAt     time.Time           `json:"detected_at"`
	Code           model.RootCauseCode `json:"code"`
	Severity       model.Severity      `json:"severity"`
	Title          string              `json:"title"`
	CanSelfResolve bool                `json:"can_self_resolve"`
	State          model.IssueState    `json:"state"`
	TicketID       string              `json:"ticket_id,omitempty"`
	VerifyAttempts int                 `json:"verify_attempts"`
	WizardData     *llm.Guide          `json:"wizard_data"`
}

// GET /api/v1/stores/{store_id}/diagnostics/wizard?lang=nl
func (s *Server) getWizard(w http.ResponseWriter, r *http.Request) {
	v, ok := s.engine.Store(r.PathValue("store_id"))
	if !ok {
		writeErr(w, diagnostics.ErrNotFound)
		return
	}
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = v.Language
	}
	resp := wizardResponse{StoreID: v.StoreID, StoreName: v.Name, Language: lang, SystemStatus: v.SystemStatus,
		Controllers: v.Controllers, LLMProvider: s.guides.ProviderName()}

	if iss := v.ActiveIssue; iss != nil {
		req := llm.GuideRequest{
			Language: lang, FaultCode: string(iss.Finding.Code), Severity: string(iss.Finding.Severity),
			SelfFixable: iss.Finding.IsSelfFixable, TargetComponent: iss.Finding.TargetModule,
			TargetPort: iss.Finding.TargetPort, ActionHint: iss.Finding.ActionHint, StoreName: v.Name,
		}
		if c := v.ActiveController; c != nil {
			req.HardwareModel, req.ControllerLocation = c.Model, c.Location
		}
		g := s.guides.Guide(r.Context(), req)
		resp.ActiveIssue = &activeIssue{
			IssueID: iss.IssueID, ControllerID: iss.ControllerID, DetectedAt: iss.OpenedAt, Code: iss.Finding.Code,
			Severity: iss.Finding.Severity, Title: g.Title, CanSelfResolve: iss.Finding.IsSelfFixable,
			State: iss.State, TicketID: iss.TicketID, VerifyAttempts: iss.VerifyAttempts, WizardData: g,
		}
	}
	writeJSON(w, 200, resp)
}

// POST /api/v1/stores/{store_id}/diagnostics/wizard/verify
func (s *Server) postVerify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IssueID      string `json:"issue_id"`
		ControllerID string `json:"controller_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body) // empty body = verify the active issue
	res, err := s.engine.Verify(r.Context(), r.PathValue("store_id"), body.IssueID, body.ControllerID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

// POST /api/v1/stores/{store_id}/tickets/escalate
func (s *Server) postEscalate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IssueID string `json:"issue_id"`
		Note    string `json:"note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	t, err := s.engine.Escalate(r.PathValue("store_id"), body.IssueID, body.Note)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, t)
}

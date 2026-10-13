package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/harveststars/roca_dashboard/internal/broker"
	"github.com/harveststars/roca_dashboard/internal/model"
	"github.com/harveststars/roca_dashboard/internal/storage"
)

var ErrNotFound = errors.New("not found")

type Config struct {
	OfflineTimeout     time.Duration // no telemetry for this long => GATEWAY_OFFLINE
	VerifyTimeout      time.Duration // how long "verify" waits for the self-test answer
	WatchdogInterval   time.Duration
	AvoidedTripCostEUR int // used for the "savings" KPI
}

// Engine keeps one lightweight state machine per controller:
//
//	HEALTHY --finding--> issue OPEN --(self-fixable)--> ACTION_REQUIRED
//	                              \--(hardware)-----> auto ticket, ESCALATED
//	issue OPEN/ESCALATED --healthy telemetry--> RESOLVED (SELF_RESOLVED | RESOLVED_BY_SERVICE)
//	issue --different root cause--> RESOLVED(SUPERSEDED) + new issue
type Engine struct {
	cfg     Config
	broker  broker.Broker
	tel     storage.TelemetryStore
	journal storage.Journal
	rules   []Rule
	log     *slog.Logger

	mu          sync.Mutex
	stores      map[string]*model.Store
	storeOrder  []string
	ctrls       map[string]*ctrlState // key: store|controller
	issues      map[string]*model.Issue
	tickets     map[string]*model.Ticket
	ticketOrder []string
	issueSeq    int
	ticketSeq   int
	waiters     map[string][]chan struct{}
	startedAt   time.Time
}

type ctrlState struct {
	storeID, controllerID string
	info                  model.ControllerInfo
	lastSeen              time.Time
	last                  *model.Telemetry
	findings              []model.Finding
	openIssueID           string
}

func key(storeID, controllerID string) string { return storeID + "|" + controllerID }

func New(cfg Config, stores []model.Store, b broker.Broker, tel storage.TelemetryStore, j storage.Journal, log *slog.Logger) (*Engine, error) {
	e := &Engine{
		cfg: cfg, broker: b, tel: tel, journal: j, rules: DefaultRules, log: log,
		stores: map[string]*model.Store{}, ctrls: map[string]*ctrlState{},
		issues: map[string]*model.Issue{}, tickets: map[string]*model.Ticket{},
		waiters: map[string][]chan struct{}{}, startedAt: time.Now(),
	}
	for i := range stores {
		s := stores[i]
		e.stores[s.StoreID] = &s
		e.storeOrder = append(e.storeOrder, s.StoreID)
		for _, c := range s.Controllers {
			e.ctrls[key(s.StoreID, c.ControllerID)] = &ctrlState{storeID: s.StoreID, controllerID: c.ControllerID, info: c}
		}
	}
	if err := e.restore(); err != nil {
		return nil, fmt.Errorf("replay journal: %w", err)
	}
	return e, nil
}

// restore rebuilds issues/tickets from the journal so a restart keeps history
// and re-attaches still-open issues to their controllers.
func (e *Engine) restore() error {
	issues, tickets, err := e.journal.Replay()
	if err != nil {
		return err
	}
	for id, iss := range issues {
		iss := iss
		e.issues[id] = &iss
		e.issueSeq = max(e.issueSeq, seqOf(id))
		if iss.State != model.IssueResolved {
			if st, ok := e.ctrls[key(iss.StoreID, iss.ControllerID)]; ok {
				st.openIssueID = id
			}
		}
	}
	for id, t := range tickets {
		t := t
		e.tickets[id] = &t
		e.ticketOrder = append(e.ticketOrder, id)
		e.ticketSeq = max(e.ticketSeq, seqOf(id))
	}
	sort.Slice(e.ticketOrder, func(i, j int) bool {
		return e.tickets[e.ticketOrder[i]].CreatedAt.Before(e.tickets[e.ticketOrder[j]].CreatedAt)
	})
	e.log.Info("journal replayed", "issues", len(issues), "tickets", len(tickets))
	return nil
}

func seqOf(id string) int {
	n, _ := strconv.Atoi(id[strings.LastIndex(id, "-")+1:])
	return n
}

// Run subscribes to the broker and runs the offline watchdog until ctx ends.
func (e *Engine) Run(ctx context.Context) error {
	e.mu.Lock()
	e.startedAt = time.Now()
	e.mu.Unlock()

	unsubT, err := e.broker.Subscribe(broker.Filter(broker.KindTelemetry), e.handleTelemetry)
	if err != nil {
		return err
	}
	defer unsubT()
	unsubE, err := e.broker.Subscribe(broker.Filter(broker.KindEvents), e.handleEvent)
	if err != nil {
		return err
	}
	defer unsubE()

	tick := time.NewTicker(e.cfg.WatchdogInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-tick.C:
			e.checkOffline(now)
		}
	}
}

// ---- Ingestion ------------------------------------------------------------

func (e *Engine) handleTelemetry(msg broker.Message) {
	storeID, ctrlID, _, ok := broker.ParseTopic(msg.Topic)
	if !ok {
		return
	}
	var t model.Telemetry
	if err := json.Unmarshal(msg.Payload, &t); err != nil {
		e.log.Warn("bad telemetry payload", "topic", msg.Topic, "err", err)
		return
	}
	// The topic (bound to the device credential in EMQX) is the identity authority.
	t.StoreID, t.ControllerID = storeID, ctrlID
	now := time.Now()
	if err := e.tel.Append(storage.TelemetryRecord{ReceivedAt: now.UTC(), Telemetry: t}); err != nil {
		e.log.Error("persist telemetry", "err", err)
	}
	findings := Evaluate(&t, e.rules, now)

	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.ctrlLocked(storeID, ctrlID)
	st.lastSeen, st.last, st.findings = now, &t, findings
	var primary *model.Finding
	if len(findings) > 0 {
		primary = &findings[0]
	}
	e.reconcileLocked(st, primary, now)

	k := key(storeID, ctrlID)
	for _, ch := range e.waiters[k] {
		close(ch)
	}
	delete(e.waiters, k)
}

func (e *Engine) handleEvent(msg broker.Message) {
	var ev model.DeviceEvent
	if err := json.Unmarshal(msg.Payload, &ev); err != nil {
		return
	}
	var ok bool
	ev.StoreID, ev.ControllerID, _, ok = broker.ParseTopic(msg.Topic)
	if !ok {
		return
	}
	e.log.Info("device event", "store", ev.StoreID, "controller", ev.ControllerID, "type", ev.Type)
	if err := e.journal.DeviceEvent(ev); err != nil {
		e.log.Error("persist device event", "err", err)
	}
	if ev.Type == model.EventDisconnected {
		e.mu.Lock()
		e.markOfflineLocked(e.ctrlLocked(ev.StoreID, ev.ControllerID), time.Now())
		e.mu.Unlock()
	}
}

// ctrlLocked returns the controller state, auto-registering devices that are
// not in stores.json (they show up on the ops dashboard as unknown stores).
func (e *Engine) ctrlLocked(storeID, ctrlID string) *ctrlState {
	k := key(storeID, ctrlID)
	if st, ok := e.ctrls[k]; ok {
		return st
	}
	s, ok := e.stores[storeID]
	if !ok {
		e.log.Warn("telemetry from unregistered store", "store", storeID)
		s = &model.Store{StoreID: storeID, Name: storeID, Language: "en"}
		e.stores[storeID] = s
		e.storeOrder = append(e.storeOrder, storeID)
	}
	info := model.ControllerInfo{ControllerID: ctrlID, Model: "unknown"}
	s.Controllers = append(s.Controllers, info)
	st := &ctrlState{storeID: storeID, controllerID: ctrlID, info: info}
	e.ctrls[k] = st
	return st
}

func (e *Engine) checkOffline(now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, st := range e.ctrls {
		ref := st.lastSeen
		if ref.IsZero() {
			ref = e.startedAt
		}
		if now.Sub(ref) <= e.cfg.OfflineTimeout {
			continue
		}
		e.markOfflineLocked(st, now)
	}
}

// markOfflineLocked raises GATEWAY_OFFLINE for a controller, either from the
// watchdog (no telemetry for OfflineTimeout) or from a DISCONNECTED event.
// The next telemetry clears it through the normal rule evaluation.
func (e *Engine) markOfflineLocked(st *ctrlState, now time.Time) {
	if len(st.findings) > 0 && st.findings[0].Code == model.RootCauseNetworkOffline {
		return
	}
	f := OfflineFinding(st.lastSeen, now)
	st.findings = []model.Finding{f}
	e.reconcileLocked(st, &f, now)
}

// ---- Issue state machine --------------------------------------------------

func (e *Engine) reconcileLocked(st *ctrlState, f *model.Finding, now time.Time) {
	cur := e.issues[st.openIssueID]
	switch {
	case f == nil:
		if cur != nil {
			e.resolveLocked(cur, "", now)
			st.openIssueID = ""
		}
	case cur != nil && cur.Finding.Code == f.Code:
		// same root cause still present: nothing to do
	default:
		if cur != nil {
			e.resolveLocked(cur, model.ResolutionSuperseded, now)
		}
		e.issueSeq++
		iss := &model.Issue{
			IssueID:      fmt.Sprintf("ISSUE-%s-%04d", now.UTC().Format("20060102"), e.issueSeq),
			StoreID:      st.storeID,
			ControllerID: st.controllerID,
			Finding:      *f,
			State:        model.IssueOpen,
			OpenedAt:     now.UTC(),
		}
		e.issues[iss.IssueID] = iss
		st.openIssueID = iss.IssueID
		e.log.Info("issue opened", "issue", iss.IssueID, "store", st.storeID, "controller", st.controllerID, "code", f.Code)
		e.journalIssue("opened", iss)
		if !f.IsSelfFixable {
			e.escalateLocked(st.storeID, st.controllerID, iss, model.TicketAuto, "Hardware fault detected by rule engine.", now)
		}
	}
}

func (e *Engine) resolveLocked(iss *model.Issue, res model.Resolution, now time.Time) {
	if res == "" {
		res = model.ResolutionSelf
		if iss.State == model.IssueEscalated {
			res = model.ResolutionService
		}
	}
	t := now.UTC()
	iss.State, iss.Resolution, iss.ResolvedAt = model.IssueResolved, res, &t
	e.log.Info("issue resolved", "issue", iss.IssueID, "resolution", res)
	e.journalIssue("resolved", iss)
	// A superseded issue keeps its ticket open: the fault is likely still
	// there underneath (e.g. hidden by the box going offline).
	if tk := e.tickets[iss.TicketID]; tk != nil && res != model.ResolutionSuperseded && tk.Status == "OPEN" {
		tk.Status, tk.ClosedAt = "CLOSED", &t
		e.journalTicket(tk)
	}
}

func (e *Engine) escalateLocked(storeID, ctrlID string, iss *model.Issue, src model.TicketSource, note string, now time.Time) *model.Ticket {
	code := model.RootCauseManagerReported
	if iss != nil {
		if tk := e.tickets[iss.TicketID]; tk != nil {
			return tk
		}
		code = iss.Finding.Code
		// Re-attach to a still-open ticket for the same fault instead of opening a duplicate.
		for _, id := range e.ticketOrder {
			tk := e.tickets[id]
			if tk.Status == "OPEN" && tk.StoreID == storeID && tk.ControllerID == ctrlID && tk.Code == code {
				e.attachTicketLocked(iss, tk)
				return tk
			}
		}
	}
	e.ticketSeq++
	tk := &model.Ticket{
		TicketID:     fmt.Sprintf("TCK-%s-%04d", now.UTC().Format("20060102"), e.ticketSeq),
		StoreID:      storeID,
		ControllerID: ctrlID,
		Code:         code,
		Source:       src,
		Status:       "OPEN",
		CreatedAt:    now.UTC(),
		Note:         note,
	}
	if iss != nil {
		tk.IssueID = iss.IssueID
		tk.RequiredParts = iss.Finding.RequiredParts
		tk.WizardAttempts = iss.VerifyAttempts
	}
	for _, c := range e.stores[storeID].Controllers {
		if st := e.ctrls[key(storeID, c.ControllerID)]; st != nil && st.last != nil {
			tk.Snapshot = append(tk.Snapshot, *st.last)
		}
	}
	e.tickets[tk.TicketID] = tk
	e.ticketOrder = append(e.ticketOrder, tk.TicketID)
	e.log.Info("ticket created", "ticket", tk.TicketID, "store", storeID, "source", src, "code", code)
	e.journalTicket(tk)
	if iss != nil {
		e.attachTicketLocked(iss, tk)
	}
	return tk
}

func (e *Engine) attachTicketLocked(iss *model.Issue, tk *model.Ticket) {
	iss.TicketID, iss.State = tk.TicketID, model.IssueEscalated
	e.journalIssue("escalated", iss)
}

func (e *Engine) journalIssue(event string, iss *model.Issue) {
	if err := e.journal.IssueEvent(event, *iss); err != nil {
		e.log.Error("persist issue", "err", err)
	}
}

func (e *Engine) journalTicket(tk *model.Ticket) {
	if err := e.journal.Ticket(*tk); err != nil {
		e.log.Error("persist ticket", "err", err)
	}
}

// ---- Store-manager actions ------------------------------------------------

type VerifyResult struct {
	Result        string       `json:"result"` // RESOLVED | STILL_FAILING | NO_RESPONSE
	Message       string       `json:"message"`
	CurrentStatus model.Status `json:"current_status"`
	IssueID       string       `json:"issue_id,omitempty"`
	ControllerID  string       `json:"controller_id,omitempty"`
}

// Verify asks the controller for a fresh self-test and waits for the answer
// (spec §2.6 #2). issueID / ctrlID may be empty: the store's active issue is used.
func (e *Engine) Verify(ctx context.Context, storeID, issueID, ctrlID string) (VerifyResult, error) {
	e.mu.Lock()
	if _, ok := e.stores[storeID]; !ok {
		e.mu.Unlock()
		return VerifyResult{}, ErrNotFound
	}
	if issueID == "" {
		if iss, _ := e.activeIssueLocked(storeID); iss != nil {
			issueID = iss.IssueID
		}
	}
	if issueID == "" {
		status := e.storeStatusLocked(storeID)
		e.mu.Unlock()
		return VerifyResult{Result: "RESOLVED", Message: "No active issue. All systems normal.", CurrentStatus: status}, nil
	}
	iss := e.issues[issueID]
	if iss == nil || iss.StoreID != storeID {
		e.mu.Unlock()
		return VerifyResult{}, ErrNotFound
	}
	if ctrlID == "" {
		ctrlID = iss.ControllerID
	}
	if iss.State != model.IssueResolved {
		iss.VerifyAttempts++
		e.journalIssue("verify_attempt", iss)
	}
	k := key(storeID, ctrlID)
	wait := make(chan struct{})
	e.waiters[k] = append(e.waiters[k], wait)
	e.mu.Unlock()

	cmd, _ := json.Marshal(model.Command{Command: model.CommandSelfTest, RequestID: issueID, IssuedAt: time.Now().Unix()})
	if err := e.broker.Publish(broker.Topic(storeID, ctrlID, broker.KindCommands), cmd); err != nil {
		return VerifyResult{}, err
	}

	answered := true
	timer := time.NewTimer(e.cfg.VerifyTimeout)
	defer timer.Stop()
	select {
	case <-wait:
	case <-timer.C:
		answered = false
	case <-ctx.Done():
		answered = false
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if !answered {
		e.dropWaiterLocked(k, wait)
	}
	res := VerifyResult{IssueID: issueID, ControllerID: ctrlID, CurrentStatus: e.storeStatusLocked(storeID)}
	switch {
	case iss.State == model.IssueResolved:
		res.Result, res.Message = "RESOLVED", "Self-test passed. Green status restored!"
	case !answered:
		res.Result, res.Message = "NO_RESPONSE", "The Rocateq box did not answer the self-test. Check its power light and network cable."
	default:
		res.Result, res.Message = "STILL_FAILING", "The problem is still there: "+iss.Finding.ActionHint
		if st := e.ctrls[k]; st != nil && len(st.findings) > 0 {
			res.Message = "The problem is still there: " + st.findings[0].ActionHint
		}
	}
	return res, nil
}

func (e *Engine) dropWaiterLocked(k string, ch chan struct{}) {
	ws := e.waiters[k]
	for i, w := range ws {
		if w == ch {
			e.waiters[k] = append(ws[:i], ws[i+1:]...)
			break
		}
	}
	if len(e.waiters[k]) == 0 {
		delete(e.waiters, k)
	}
}

// Escalate creates (or returns the existing) qualified dispatch ticket
// (spec §2.6 #3). Without an issue the manager can still report a problem.
func (e *Engine) Escalate(storeID, issueID, note string) (model.Ticket, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, ok := e.stores[storeID]
	if !ok {
		return model.Ticket{}, ErrNotFound
	}
	var iss *model.Issue
	if issueID != "" {
		iss = e.issues[issueID]
		if iss == nil || iss.StoreID != storeID {
			return model.Ticket{}, ErrNotFound
		}
	} else {
		iss, _ = e.activeIssueLocked(storeID)
	}
	if iss != nil && iss.State == model.IssueResolved {
		iss = nil
	}
	ctrlID := ""
	if iss != nil {
		ctrlID = iss.ControllerID
	} else if len(s.Controllers) > 0 {
		ctrlID = s.Controllers[0].ControllerID
	}
	if note == "" {
		note = "Store manager could not resolve the issue with the self-help wizard."
	}
	return *e.escalateLocked(storeID, ctrlID, iss, model.TicketManager, note, time.Now()), nil
}

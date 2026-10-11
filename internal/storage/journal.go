package storage

import (
	"path/filepath"
	"time"

	"github.com/harveststars/roca_dashboard/internal/model"
)

// Journal persists issue / ticket lifecycle and raw device events. Every write
// is a full snapshot of the entity, so replay is "last line per id wins".
type Journal interface {
	IssueEvent(event string, issue model.Issue) error
	Ticket(t model.Ticket) error
	DeviceEvent(e model.DeviceEvent) error
	Replay() (issues map[string]model.Issue, tickets map[string]model.Ticket, err error)
	Close() error
}

type IssueRecord struct {
	At    time.Time   `json:"at"`
	Event string      `json:"event"` // opened | verify_attempt | escalated | resolved
	Issue model.Issue `json:"issue"`
}

type FileJournal struct {
	dir                   string
	issues, tickets, devs jsonlWriter
}

func NewFileJournal(logDir string) *FileJournal { return &FileJournal{dir: logDir} }

func (j *FileJournal) path(name string) string { return filepath.Join(j.dir, name) }

func (j *FileJournal) IssueEvent(event string, issue model.Issue) error {
	return j.issues.write(j.path("issues.jsonl"), IssueRecord{At: time.Now().UTC(), Event: event, Issue: issue})
}

func (j *FileJournal) Ticket(t model.Ticket) error {
	return j.tickets.write(j.path("tickets.jsonl"), t)
}

func (j *FileJournal) DeviceEvent(e model.DeviceEvent) error {
	return j.devs.write(j.path("device_events.jsonl"), e)
}

func (j *FileJournal) Replay() (map[string]model.Issue, map[string]model.Ticket, error) {
	issues := map[string]model.Issue{}
	tickets := map[string]model.Ticket{}
	if err := readJSONL(j.path("issues.jsonl"), func(r IssueRecord) { issues[r.Issue.IssueID] = r.Issue }); err != nil {
		return nil, nil, err
	}
	if err := readJSONL(j.path("tickets.jsonl"), func(t model.Ticket) { tickets[t.TicketID] = t }); err != nil {
		return nil, nil, err
	}
	return issues, tickets, nil
}

func (j *FileJournal) Close() error {
	j.issues.close()
	j.tickets.close()
	return j.devs.close()
}

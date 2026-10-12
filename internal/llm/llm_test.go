package llm

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestTemplateFillsPlaceholders(t *testing.T) {
	g, err := Template{}.GenerateGuide(context.Background(), GuideRequest{
		Language: "nl", FaultCode: "LOOP_CABLE_DISCONNECTED", TargetPort: "J2", ControllerLocation: "onder kassa 1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Steps) != 3 || !strings.Contains(g.Steps[1].Title, "J2") || !strings.Contains(g.Steps[0].Instruction, "onder kassa 1") {
		t.Fatalf("unexpected guide: %+v", g)
	}
}

func TestTemplateFallsBackToEnglish(t *testing.T) {
	g, _ := Template{}.GenerateGuide(context.Background(), GuideRequest{Language: "es", FaultCode: "GATEWAY_OFFLINE"})
	if g.Language != "en" {
		t.Fatalf("language=%s", g.Language)
	}
}

type fakeProvider struct {
	calls chan struct{}
	err   error
}

func (f *fakeProvider) Name() string { return "fake" }
func (f *fakeProvider) GenerateGuide(context.Context, GuideRequest) (*Guide, error) {
	f.calls <- struct{}{}
	if f.err != nil {
		return nil, f.err
	}
	return &Guide{Title: "from llm", Steps: []GuideStep{{StepNumber: 1}}}, nil
}

func TestServiceServesTemplateUntilLLMReady(t *testing.T) {
	fp := &fakeProvider{calls: make(chan struct{}, 10)}
	s := NewGuideService(fp, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := GuideRequest{Language: "fr", FaultCode: "POWER_SUPPLY_ABNORMAL"}
	if g := s.Guide(context.Background(), req); g.Source != "template" || g.Language != "fr" {
		t.Fatalf("first call should be template, got %+v", g)
	}
	<-fp.calls
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if g := s.Guide(context.Background(), req); g.Source == "fake" {
			if len(fp.calls) != 0 {
				t.Fatalf("provider called more than once")
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("llm guide never served")
}

func TestServiceKeepsTemplateWhenProviderFails(t *testing.T) {
	fp := &fakeProvider{calls: make(chan struct{}, 10), err: ErrNotWired}
	s := NewGuideService(fp, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := GuideRequest{Language: "de", FaultCode: "GATEWAY_OFFLINE"}
	s.Guide(context.Background(), req)
	<-fp.calls
	time.Sleep(20 * time.Millisecond)
	if g := s.Guide(context.Background(), req); g.Source != "template" {
		t.Fatalf("got %+v", g)
	}
	if len(fp.calls) != 0 {
		t.Fatal("failed provider retried immediately")
	}
}

func TestParseReceiptJSONNormalises(t *testing.T) {
	r, err := ParseReceiptJSON(`{"merchant_name":"X","transaction_date":"2026-10-12","currency":"eur","total_amount":12.5,"expense_category":"parking","travel_segment":{}}`)
	if err != nil || r.Currency != "EUR" || r.ExpenseCategory != "OTHER" || r.TravelSegment != nil || r.FlagManualReview {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	r, _ = ParseReceiptJSON(`{"merchant_name":"X","currency":"EUR","total_amount":0}`)
	if !r.FlagManualReview {
		t.Fatal("missing total/date must be flagged")
	}
}

func TestParseGuideJSON(t *testing.T) {
	g, err := ParseGuideJSON("```json\n{\"title\":\"x\",\"status_color\":\"RED\",\"steps\":[{\"step_number\":1,\"title\":\"a\",\"instruction\":\"b\"}]}\n```")
	if err != nil || g.Title != "x" {
		t.Fatalf("g=%+v err=%v", g, err)
	}
}

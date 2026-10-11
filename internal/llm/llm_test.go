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

func TestServiceFallsBackWhenProviderNotWired(t *testing.T) {
	s := NewGuideService(&OpenAI{APIKey: "x"}, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	g := s.Guide(context.Background(), GuideRequest{Language: "fr", FaultCode: "POWER_SUPPLY_ABNORMAL"})
	if g.Source != "template" || g.Language != "fr" {
		t.Fatalf("got %+v", g)
	}
}

func TestParseGuideJSON(t *testing.T) {
	g, err := ParseGuideJSON("```json\n{\"title\":\"x\",\"status_color\":\"RED\",\"steps\":[{\"step_number\":1,\"title\":\"a\",\"instruction\":\"b\"}]}\n```")
	if err != nil || g.Title != "x" {
		t.Fatalf("g=%+v err=%v", g, err)
	}
}

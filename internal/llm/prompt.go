package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

var languageNames = map[string]string{"en": "English", "nl": "Dutch", "fr": "French", "de": "German"}

// verifyButton is the label of the store UI's verify button (web/store/app.js),
// so the guide refers to the button the manager actually sees.
var verifyButton = map[string]string{"en": "Verify solution", "nl": "Opnieuw testen", "fr": "Vérifier", "de": "Lösung prüfen"}

// BuildGuidePrompt renders the system/user prompt of spec §2.5. Shared by all
// remote providers so the output contract stays identical.
func BuildGuidePrompt(req GuideRequest) (system, user string) {
	system = "You are a helpful B2B store maintenance wizard for Rocateq retail anti-theft systems. " +
		"You write for non-technical supermarket managers. Never suggest opening sealed hardware. " +
		"Reply with pure JSON only, no markdown."

	lang := languageNames[req.Language]
	if lang == "" {
		lang = "English"
	}
	button := verifyButton[req.Language]
	if button == "" {
		button = verifyButton["en"]
	}
	task := fmt.Sprintf("Output a concise, friendly, 3-step action guide for the store manager to fix the problem themselves, ending with pressing the button labelled '%s'.", button)
	if !req.SelfFixable {
		task = "This is a hardware fault the store must NOT try to repair. Output 2 short steps reassuring the manager that a Rocateq technician has been notified with the right spare part, and what to do meanwhile."
	}
	user = fmt.Sprintf(`Input Context:
- Store: %s
- Store Language: %s (%s)
- Detected Fault: %s (severity %s)
- Hardware Model: %s
- Controller Location: %s
- Target Component: %s
- Technical Hint: %s
Task: %s
Return JSON with exactly this shape:
{"title": string, "status_color": "RED"|"ORANGE", "estimated_fix_time": string,
 "steps": [{"step_number": int, "title": string, "instruction": string,
            "visual_hint_icon": "cabinet_open"|"cable_plug"|"power_plug"|"network_cable"|"refresh_check"|"technician"|"info"}]}`,
		req.StoreName, req.Language, lang, req.FaultCode, req.Severity, req.HardwareModel,
		req.ControllerLocation, req.TargetComponent, req.ActionHint, task)
	return system, user
}

// ParseGuideJSON validates a model response. Tolerates a ```json fence.
func ParseGuideJSON(text string) (*Guide, error) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	var g Guide
	if err := json.Unmarshal([]byte(text), &g); err != nil {
		return nil, fmt.Errorf("llm guide: invalid json: %w", err)
	}
	if g.Title == "" || len(g.Steps) == 0 {
		return nil, fmt.Errorf("llm guide: missing title or steps")
	}
	return &g, nil
}

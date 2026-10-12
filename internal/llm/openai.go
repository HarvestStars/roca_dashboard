package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAI talks to the Chat Completions API with plain net/http (no SDK).
// The API key is only ever sent to BaseURL in the Authorization header.
type OpenAI struct {
	APIKey, Model, BaseURL string
	HTTP                   *http.Client
}

func (o *OpenAI) Name() string { return "openai:" + o.model() }

func (o *OpenAI) model() string {
	if o.Model == "" {
		return "gpt-5.4-mini"
	}
	return o.Model
}

type chatPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []chatPart
}

// chatJSON sends one request in JSON mode and returns the assistant text.
func (o *OpenAI) chatJSON(ctx context.Context, msgs []chatMessage) (string, error) {
	if o.APIKey == "" {
		return "", errors.New("openai: API key not set (OPENAI_API_KEY)")
	}
	body := map[string]any{
		"model":           o.model(),
		"messages":        msgs,
		"response_format": map[string]string{"type": "json_object"},
	}
	// Reasoning models: keep latency demo-friendly.
	if m := o.model(); strings.HasPrefix(m, "gpt-5") || strings.HasPrefix(m, "o") {
		body["reasoning_effort"] = "low"
	}
	b, _ := json.Marshal(body)

	base := strings.TrimRight(o.BaseURL, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+o.APIKey)
	req.Header.Set("Content-Type", "application/json")

	client := o.HTTP
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
				Refusal string `json:"refusal"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("openai: http %d: undecodable response", resp.StatusCode)
	}
	if out.Error != nil {
		return "", fmt.Errorf("openai: http %d: %s", resp.StatusCode, out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("openai: http %d: no choices", resp.StatusCode)
	}
	if r := out.Choices[0].Message.Refusal; r != "" {
		return "", fmt.Errorf("openai: refused: %s", r)
	}
	return out.Choices[0].Message.Content, nil
}

func (o *OpenAI) GenerateGuide(ctx context.Context, req GuideRequest) (*Guide, error) {
	system, user := BuildGuidePrompt(req)
	text, err := o.chatJSON(ctx, []chatMessage{{Role: "system", Content: system}, {Role: "user", Content: user}})
	if err != nil {
		return nil, err
	}
	g, err := ParseGuideJSON(text)
	if err != nil {
		return nil, err
	}
	for i := range g.Steps {
		g.Steps[i].StepNumber = i + 1
	}
	return g, nil
}

func (o *OpenAI) ExtractReceipt(ctx context.Context, image []byte, mime string) (*Receipt, error) {
	dataURL := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(image)
	text, err := o.chatJSON(ctx, []chatMessage{
		{Role: "system", Content: receiptSystemPrompt},
		{Role: "user", Content: []chatPart{
			{Type: "text", Text: receiptUserPrompt},
			{Type: "image_url", ImageURL: &imageURL{URL: dataURL, Detail: "high"}},
		}},
	})
	if err != nil {
		return nil, err
	}
	return ParseReceiptJSON(text)
}

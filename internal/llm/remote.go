package llm

import (
	"context"
	"fmt"
)

// Anthropic shares the prompt/parse contract in prompt.go and receipt.go.
// To go live, implement the HTTP call marked TODO (Messages API, image as a
// base64 "image" content block) and return ParseGuideJSON / ParseReceiptJSON.
type Anthropic struct {
	APIKey, Model, BaseURL string
}

func (a *Anthropic) Name() string { return "anthropic" }

func (a *Anthropic) GenerateGuide(ctx context.Context, req GuideRequest) (*Guide, error) {
	if a.APIKey == "" {
		return nil, fmt.Errorf("anthropic: LLM_API_KEY not set")
	}
	_, _ = BuildGuidePrompt(req)
	// TODO: POST {BaseURL|https://api.anthropic.com}/v1/messages
	//       (headers x-api-key, anthropic-version), then ParseGuideJSON.
	return nil, ErrNotWired
}

func (a *Anthropic) ExtractReceipt(ctx context.Context, image []byte, mime string) (*Receipt, error) {
	return nil, ErrNotWired
}

package llm

import (
	"context"
	"fmt"
)

// OpenAI and Anthropic share the prompt/parse contract in prompt.go. To go
// live, implement the HTTP call marked TODO and return ParseGuideJSON(text).
// Until then they return ErrNotWired and GuideService falls back to Template.

type OpenAI struct {
	APIKey, Model, BaseURL string
}

func (o *OpenAI) Name() string { return "openai" }

func (o *OpenAI) GenerateGuide(ctx context.Context, req GuideRequest) (*Guide, error) {
	if o.APIKey == "" {
		return nil, fmt.Errorf("openai: LLM_API_KEY not set")
	}
	_, _ = BuildGuidePrompt(req)
	// TODO: POST {BaseURL|https://api.openai.com/v1}/chat/completions
	//       with response_format={"type":"json_object"}, then ParseGuideJSON.
	return nil, ErrNotWired
}

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

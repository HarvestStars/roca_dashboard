// Package llm turns a technical root cause into a short, friendly,
// multi-language guide for a non-technical store manager (spec §2.5).
//
// Providers are pluggable. The demo runs on the built-in "template" provider
// (no network, deterministic). "openai" and "anthropic" are wired through the
// same prompt/response contract and only need their HTTP call filled in.
package llm

import (
	"context"
	"errors"
)

type GuideRequest struct {
	Language           string // en | nl | fr | de
	FaultCode          string
	Severity           string
	SelfFixable        bool
	HardwareModel      string
	TargetComponent    string
	TargetPort         string
	ControllerLocation string
	ActionHint         string
	StoreName          string
}

// Guide is the standardized JSON rendered by the store UI (spec §2.5).
type Guide struct {
	Title            string      `json:"title"`
	StatusColor      string      `json:"status_color"` // RED | ORANGE | GREEN
	EstimatedFixTime string      `json:"estimated_fix_time"`
	Steps            []GuideStep `json:"steps"`
	Language         string      `json:"language"`
	Source           string      `json:"source"` // provider that produced it
}

type GuideStep struct {
	StepNumber     int    `json:"step_number"`
	Title          string `json:"title"`
	Instruction    string `json:"instruction"`
	VisualHintIcon string `json:"visual_hint_icon"`
}

type Provider interface {
	Name() string
	GenerateGuide(ctx context.Context, req GuideRequest) (*Guide, error)
}

var ErrNotWired = errors.New("llm provider HTTP call not wired yet")

// Config selects and configures a provider. Populated from env in main.
type Config struct {
	Provider string // template | openai | anthropic
	APIKey   string
	Model    string
	BaseURL  string
}

// NewProvider builds the configured provider.
func NewProvider(cfg Config) Provider {
	switch cfg.Provider {
	case "openai":
		return &OpenAI{APIKey: cfg.APIKey, Model: cfg.Model, BaseURL: cfg.BaseURL}
	case "anthropic", "claude":
		return &Anthropic{APIKey: cfg.APIKey, Model: cfg.Model, BaseURL: cfg.BaseURL}
	default:
		return Template{}
	}
}

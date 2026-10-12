package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Receipt is the structured extraction of one receipt photo (spec §3.3),
// extended with times and travel segments so trip hours can later be
// cross-checked against receipts (e.g. outbound/return flight envelope).
type Receipt struct {
	MerchantName     string         `json:"merchant_name"`
	TransactionDate  string         `json:"transaction_date"`           // YYYY-MM-DD
	TransactionTime  string         `json:"transaction_time,omitempty"` // HH:MM, local time printed on the receipt
	Currency         string         `json:"currency"`
	TotalAmount      float64        `json:"total_amount"`
	VATAmount        float64        `json:"vat_amount"`
	ExpenseCategory  string         `json:"expense_category"`
	ConfidenceScore  float64        `json:"confidence_score"`
	FlagManualReview bool           `json:"flag_manual_review"`
	ReviewReason     string         `json:"review_reason,omitempty"`
	LineItemsSummary string         `json:"line_items_summary"`
	TravelSegment    *TravelSegment `json:"travel_segment,omitempty"`
}

// TravelSegment is filled for flights/trains/taxis when the receipt shows them.
type TravelSegment struct {
	From          string `json:"from,omitempty"`
	To            string `json:"to,omitempty"`
	DepartureTime string `json:"departure_time,omitempty"` // YYYY-MM-DDTHH:MM local
	ArrivalTime   string `json:"arrival_time,omitempty"`
}

var ExpenseCategories = []string{"HOTEL", "TRANSPORT_FLIGHT_TRAIN", "TAXI", "MEAL", "TOOLS_SUPPLIES", "OTHER"}

// ReceiptExtractor reads a receipt photo. There is deliberately no offline
// "template" implementation: without a vision model the expense goes to
// manual entry instead of being filled with invented data.
type ReceiptExtractor interface {
	Name() string
	ExtractReceipt(ctx context.Context, image []byte, mime string) (*Receipt, error)
}

var ErrNoVisionModel = errors.New("no vision LLM configured: please enter the receipt manually")

// NoVision is used when no API key is configured.
type NoVision struct{}

func (NoVision) Name() string { return "none" }
func (NoVision) ExtractReceipt(context.Context, []byte, string) (*Receipt, error) {
	return nil, ErrNoVisionModel
}

// NewReceiptExtractor returns the vision-capable provider for cfg.
func NewReceiptExtractor(cfg Config) ReceiptExtractor {
	if p, ok := NewProvider(cfg).(ReceiptExtractor); ok && cfg.APIKey != "" {
		return p
	}
	return NoVision{}
}

const receiptSystemPrompt = `You are an automated European accounting receipt parser for Rocateq, a company whose field engineers travel to supermarkets across Europe.
Extract financial data from the attached receipt image into strict JSON. Read only what is printed; never guess missing values.
Rules:
- Identify the currency (EUR, USD, GBP, ...). Use ISO 4217 codes.
- Amounts in standard decimal notation (e.g. 1.234,50 -> 1234.50). total_amount is the final amount paid; vat_amount is the total VAT/MwSt/BTW/TVA (0 if none shown).
- expense_category is exactly one of: HOTEL, TRANSPORT_FLIGHT_TRAIN, TAXI, MEAL, TOOLS_SUPPLIES, OTHER. Parking goes to OTHER.
- transaction_date as YYYY-MM-DD; transaction_time as HH:MM if printed.
- For flights, trains and taxis fill travel_segment (from, to, departure_time, arrival_time as YYYY-MM-DDTHH:MM) when shown.
- confidence_score in [0,1] reflects how legible and unambiguous the receipt is.
- If total or date is unreadable or ambiguous, set flag_manual_review=true and explain in review_reason.
- Do not copy personal data (names, addresses, card numbers) into line_items_summary; summarise what was bought in one short English sentence.`

const receiptUserPrompt = `Parse this receipt. Return JSON with exactly these keys:
{"merchant_name": string, "transaction_date": string, "transaction_time": string, "currency": string,
 "total_amount": number, "vat_amount": number, "expense_category": string, "confidence_score": number,
 "flag_manual_review": boolean, "review_reason": string, "line_items_summary": string,
 "travel_segment": {"from": string, "to": string, "departure_time": string, "arrival_time": string} | null}`

// ParseReceiptJSON validates a model response.
func ParseReceiptJSON(text string) (*Receipt, error) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	var r Receipt
	if err := json.Unmarshal([]byte(text), &r); err != nil {
		return nil, fmt.Errorf("llm receipt: invalid json: %w", err)
	}
	r.Currency = strings.ToUpper(strings.TrimSpace(r.Currency))
	r.ExpenseCategory = strings.ToUpper(strings.TrimSpace(r.ExpenseCategory))
	if !validCategory(r.ExpenseCategory) {
		r.ExpenseCategory = "OTHER"
	}
	if r.TravelSegment != nil && *r.TravelSegment == (TravelSegment{}) {
		r.TravelSegment = nil
	}
	if r.TotalAmount <= 0 || r.TransactionDate == "" {
		r.FlagManualReview = true
		if r.ReviewReason == "" {
			r.ReviewReason = "total or date missing"
		}
	}
	return &r, nil
}

func validCategory(c string) bool {
	for _, x := range ExpenseCategories {
		if c == x {
			return true
		}
	}
	return false
}

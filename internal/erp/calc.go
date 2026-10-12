package erp

import (
	"math"
	"sort"
	"time"

	"github.com/harveststars/roca_dashboard/internal/llm"
)

// GeoWarnKm: a punch farther than this from the store is flagged for review
// (not blocked — GPS indoors is often poor).
const GeoWarnKm = 1.0

// OnsiteHours sums CHECK_IN..CHECK_OUT pairs (v1 rule agreed with the
// business: hours = time between check-in and check-out). An open check-in is
// counted until now and reported via onSite.
func OnsiteHours(punches []Punch, now time.Time) (hours float64, onSite bool) {
	var in *time.Time
	var total time.Duration
	for i := range punches {
		p := punches[i]
		switch p.Action {
		case ActionCheckIn:
			in = &punches[i].At
		case ActionCheckOut:
			if in != nil {
				total += p.At.Sub(*in)
				in = nil
			}
		}
	}
	if in != nil {
		total += now.Sub(*in)
		onSite = true
	}
	return round2(total.Hours()), onSite
}

// statusFromPunches derives the working status before submission.
func statusFromPunches(punches []Punch) TripStatus {
	if len(punches) == 0 {
		return TripAssigned
	}
	if punches[len(punches)-1].Action == ActionCheckIn {
		return TripOnSite
	}
	return TripWorkDone
}

func haversineKm(lat1, lng1, lat2, lng2 float64) float64 {
	const r = 6371.0
	rad := math.Pi / 180
	dLat, dLng := (lat2-lat1)*rad, (lng2-lng1)*rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * r * math.Asin(math.Sqrt(a))
}

// receiptEnvelope collects every date+time printed on the receipts and
// returns the earliest and latest. Receipt times are local wall-clock times
// without zone, so they are compared as naive times.
func receiptEnvelope(expenses []Expense) *TimeEnvelope {
	var ts []time.Time
	add := func(s string) {
		// Date-only values carry no hour information and would read as midnight.
		for _, layout := range []string{"2006-01-02T15:04", "2006-01-02 15:04"} {
			if t, err := time.Parse(layout, s); err == nil {
				ts = append(ts, t)
				return
			}
		}
	}
	for _, e := range expenses {
		r := e.Final
		if r == nil {
			continue
		}
		if r.TransactionTime != "" {
			add(r.TransactionDate + "T" + r.TransactionTime)
		}
		if seg := r.TravelSegment; seg != nil {
			add(seg.DepartureTime)
			add(seg.ArrivalTime)
		}
	}
	if len(ts) < 2 {
		return nil
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i].Before(ts[j]) })
	first, last := ts[0], ts[len(ts)-1]
	return &TimeEnvelope{First: first.Format("2006-01-02 15:04"), Last: last.Format("2006-01-02 15:04"),
		SpanHours: round2(last.Sub(first).Hours())}
}

func totals(expenses []Expense) ExpenseTotals {
	t := ExpenseTotals{ByCategory: map[string]float64{}}
	for _, e := range expenses {
		t.ReceiptCount++
		if e.Pending() {
			t.ReceiptsPendingReview++
		}
		if e.Final == nil {
			continue
		}
		// v1: every amount is treated as EUR (no FX conversion), see design doc.
		t.TotalEUR += e.Final.TotalAmount
		t.VATEUR += e.Final.VATAmount
		t.ByCategory[e.Final.ExpenseCategory] += e.Final.TotalAmount
	}
	t.TotalEUR, t.VATEUR = round2(t.TotalEUR), round2(t.VATEUR)
	for k, v := range t.ByCategory {
		t.ByCategory[k] = round2(v)
	}
	return t
}

// reviewStatus decides whether an extraction can be accepted without a human.
func reviewStatus(r *llm.Receipt) ExpenseStatus {
	if r.FlagManualReview || r.ConfidenceScore < AutoAcceptConfidence {
		return ExpenseNeedsReview
	}
	return ExpenseExtracted
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }

package ats

import (
	"fmt"
	"math"
	"strings"
)

// Pay sources. An empty Source means pay is unknown.
const (
	PayFromField       = "field"       // a structured field in the ATS response
	PayFromDescription = "description" // parsed from description text, lower confidence
)

// Pay periods.
const (
	PerYear  = "year"
	PerMonth = "month"
	PerWeek  = "week"
	PerDay   = "day"
	PerHour  = "hour"
)

// Pay is a posted compensation range. Zero Min or Max means that end was
// not stated. Unknown pay is the zero value, never a guess.
type Pay struct {
	Min      float64
	Max      float64
	Currency string // ISO 4217 when known, e.g. "USD"
	Period   string // one of the Per* constants, or "" when unstated
	Source   string // PayFromField, PayFromDescription, or "" for unknown
}

// Known reports whether any pay was captured.
func (p Pay) Known() bool { return p.Source != "" }

// Annual returns the midpoint scaled to a year (2080 hours, 52 weeks,
// 260 days, 12 months). Zero when unknown.
func (p Pay) Annual() float64 {
	if !p.Known() {
		return 0
	}
	mid := p.Max
	if p.Min > 0 && p.Max > 0 {
		mid = (p.Min + p.Max) / 2
	} else if p.Min > 0 {
		mid = p.Min
	}
	switch p.Period {
	case PerHour:
		return mid * 2080
	case PerDay:
		return mid * 260
	case PerWeek:
		return mid * 52
	case PerMonth:
		return mid * 12
	}
	return mid
}

// String renders pay for humans: "USD 150k-180k/year (field)" or
// "pay unknown".
func (p Pay) String() string {
	if !p.Known() {
		return "pay unknown"
	}
	var amt string
	switch {
	case p.Min > 0 && p.Max > 0 && p.Min != p.Max:
		amt = short(p.Min) + "-" + short(p.Max)
	case p.Max > 0:
		amt = short(p.Max)
	default:
		amt = short(p.Min)
	}
	cur := p.Currency
	if cur == "" {
		cur = "?"
	}
	s := cur + " " + amt
	if p.Period != "" {
		s += "/" + p.Period
	}
	return s + " (" + p.Source + ")"
}

func short(v float64) string {
	if v >= 1000 {
		k := v / 1000
		if k == math.Trunc(k) {
			return fmt.Sprintf("%.0fk", k)
		}
		return fmt.Sprintf("%.1fk", k)
	}
	if v == math.Trunc(v) {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.2f", v)
}

// PeriodFromText maps an ATS interval label such as "1 YEAR" or
// "per-hour-wage" to a Per* constant, or "" when it names none.
func PeriodFromText(s string) string {
	s = strings.ToLower(s)
	for _, p := range []struct{ word, period string }{
		{"year", PerYear}, {"annual", PerYear}, {"month", PerMonth},
		{"week", PerWeek}, {"day", PerDay}, {"daily", PerDay}, {"hour", PerHour},
	} {
		if strings.Contains(s, p.word) {
			return p.period
		}
	}
	return ""
}

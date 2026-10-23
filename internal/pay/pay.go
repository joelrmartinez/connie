// Package pay finds a pay range in posting description text, for boards
// whose ATS response has no structured pay. Text is messy, so the parser
// is strict: it wants a currency marker and a plausible amount, and it
// returns nothing rather than a guess. Results are marked as coming from
// the description so ranking can treat them as lower confidence.
package pay

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/joelrmartinez/connie/ats"
)

// An amount: a currency marker before (symbol or code), digits with
// optional thousands separators and decimals, and an optional k.
const amount = `(?:(US\$|CA\$|C\$|A\$|\$|£|€|USD|CAD|EUR|GBP|AUD)\s?)?(\d{1,3}(?:,\d{3})+|\d+)(?:\.(\d+))?\s?([kK])?`

var (
	// A range: amount, a dash (hyphen, en dash, em dash) or "to", amount,
	// an optional trailing currency code, then an optional period.
	rangeRe = regexp.MustCompile(amount + `\s*(?:-|\x{2013}|\x{2014}|to)\s*` + amount +
		`(?:\s?(USD|CAD|EUR|GBP|AUD))?` + periodTail)
	// A single amount counts only with a stated period, or right after a
	// word like salary, so "$5M Series A" and "401(k)" never match.
	singleRe = regexp.MustCompile(`(?i:(salary|pay|compensation|base|wage|rate|ote)[^.$£€\d]{0,30})?` +
		amount + `(?:\s?(USD|CAD|EUR|GBP|AUD))?` + periodTail)
)

// periodTail captures "per year", "/hr", "annually", "an hour", and so on.
const periodTail = `(?:\s*(?i:(?:per|/|an?|each)\s*)?(?i:(year|yr|annum|annual(?:ly)?|month|mo|week|wk|day|hour|hr|hourly))\b)?`

var codeFor = map[string]string{
	"$": "USD", "US$": "USD", "USD": "USD", "CA$": "CAD", "C$": "CAD", "CAD": "CAD",
	"A$": "AUD", "AUD": "AUD", "£": "GBP", "GBP": "GBP", "€": "EUR", "EUR": "EUR",
}

// Parse returns the first plausible pay range in text, with Source set to
// ats.PayFromDescription, or ok=false.
func Parse(text string) (p ats.Pay, ok bool) {
	for _, m := range rangeRe.FindAllStringSubmatch(text, -1) {
		cur1, lo := m[1], value(m[2], m[3], m[4])
		cur2, hi := m[5], value(m[6], m[7], m[8])
		code, period := m[9], periodOf(m[10])
		// A k on only the high end applies to both: "$150-180k".
		if m[4] == "" && m[8] != "" && lo < 1000 {
			lo *= 1000
		}
		cur := firstNonEmpty(codeFor[cur1], codeFor[cur2], codeFor[strings.ToUpper(code)])
		if cur == "" || lo <= 0 || hi < lo {
			continue
		}
		if period == "" {
			period = guessPeriod(hi)
		}
		if plausible(lo, period) && plausible(hi, period) {
			return ats.Pay{Min: lo, Max: hi, Currency: cur, Period: period, Source: ats.PayFromDescription}, true
		}
	}
	for _, m := range singleRe.FindAllStringSubmatch(text, -1) {
		cue, cur := m[1], firstNonEmpty(codeFor[m[2]], codeFor[strings.ToUpper(m[6])])
		v, period := value(m[3], m[4], m[5]), periodOf(m[7])
		if cur == "" || (period == "" && cue == "") {
			continue
		}
		if period == "" {
			period = guessPeriod(v)
		}
		if plausible(v, period) {
			return ats.Pay{Min: v, Max: v, Currency: cur, Period: period, Source: ats.PayFromDescription}, true
		}
	}
	return ats.Pay{}, false
}

func value(whole, frac, k string) float64 {
	v, err := strconv.ParseFloat(strings.ReplaceAll(whole, ",", "")+"."+frac+"0", 64)
	if err != nil {
		return 0
	}
	if k != "" {
		v *= 1000
	}
	return v
}

func periodOf(s string) string {
	switch strings.ToLower(s) {
	case "":
		return ""
	case "mo":
		return ats.PerMonth
	case "wk":
		return ats.PerWeek
	case "hr", "hourly":
		return ats.PerHour
	case "yr", "annum":
		return ats.PerYear
	}
	return ats.PeriodFromText(s)
}

// guessPeriod reads an unlabeled amount: hourly when small, else annual.
func guessPeriod(v float64) string {
	if v < 1000 {
		return ats.PerHour
	}
	return ats.PerYear
}

// plausible rejects amounts that cannot be pay for the period, such as a
// funding round or a team size.
func plausible(v float64, period string) bool {
	switch period {
	case ats.PerHour:
		return v >= 7 && v <= 1000
	case ats.PerDay:
		return v >= 50 && v <= 5000
	case ats.PerWeek:
		return v >= 300 && v <= 25000
	case ats.PerMonth:
		return v >= 1000 && v <= 100000
	default:
		return v >= 15000 && v <= 2000000
	}
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// Package export writes scan results as CSV (UTF-8 BOM for Excel) and JSON,
// in a deterministic order so diffs between runs are meaningful.
package export

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/joelrmartinez/connie/ats"
	"github.com/joelrmartinez/connie/internal/store"
)

var header = []string{"ats", "board", "external_id", "title", "location",
	"department", "team", "employment_type", "remote", "url", "published_at", "matched",
	"pay_min", "pay_max", "pay_currency", "pay_period", "pay_source", "pay_flag"}

// jsonRow fixes the JSON field names independent of internal struct shape.
type jsonRow struct {
	ATS            string   `json:"ats"`
	Board          string   `json:"board"`
	ExternalID     string   `json:"external_id"`
	Title          string   `json:"title"`
	Location       string   `json:"location"`
	Department     string   `json:"department"`
	Team           string   `json:"team"`
	EmploymentType string   `json:"employment_type"`
	Remote         bool     `json:"remote"`
	URL            string   `json:"url"`
	PublishedAt    string   `json:"published_at"`
	Matched        string   `json:"matched"`
	Pay            *jsonPay `json:"pay"` // null when unknown
	PayFlag        string   `json:"pay_flag"`
}

type jsonPay struct {
	Min      float64 `json:"min,omitempty"`
	Max      float64 `json:"max,omitempty"`
	Currency string  `json:"currency"`
	Period   string  `json:"period"`
	Source   string  `json:"source"`
}

// payFlag marks rows whose pay needs a human look: "unknown" when none
// was found, "parsed" when it came from description text, "" when a
// structured field stated it.
func payFlag(p ats.Pay) string {
	switch p.Source {
	case "":
		return "unknown"
	case ats.PayFromDescription:
		return "parsed"
	}
	return ""
}

func amount(v float64) string {
	if v == 0 {
		return ""
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// CSV writes rows as UTF-8-BOM CSV with a fixed column order.
func CSV(w io.Writer, rows []store.Row) error {
	if _, err := io.WriteString(w, "\uFEFF"); err != nil {
		return fmt.Errorf("write BOM: %w", err)
	}
	cw := csv.NewWriter(w)
	if err := cw.Write(header); err != nil {
		return fmt.Errorf("write CSV header: %w", err)
	}
	for _, r := range sorted(rows) {
		record := []string{r.ATS, r.Board, r.ExternalID, r.Title, r.Location,
			r.Department, r.Team, r.EmploymentType, strconv.FormatBool(r.Remote),
			r.URL, published(r), r.Matched,
			amount(r.Pay.Min), amount(r.Pay.Max), r.Pay.Currency, r.Pay.Period, r.Pay.Source, payFlag(r.Pay)}
		if err := cw.Write(record); err != nil {
			return fmt.Errorf("write CSV row %q: %w", r.Key(), err)
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("flush CSV: %w", err)
	}
	return nil
}

// JSON writes rows as a JSON array with stable field names.
func JSON(w io.Writer, rows []store.Row) error {
	out := make([]jsonRow, 0, len(rows))
	for _, r := range sorted(rows) {
		row := jsonRow{
			ATS: r.ATS, Board: r.Board, ExternalID: r.ExternalID, Title: r.Title,
			Location: r.Location, Department: r.Department, Team: r.Team,
			EmploymentType: r.EmploymentType, Remote: r.Remote, URL: r.URL,
			PublishedAt: published(r), Matched: r.Matched, PayFlag: payFlag(r.Pay),
		}
		if r.Pay.Known() {
			row.Pay = &jsonPay{Min: r.Pay.Min, Max: r.Pay.Max, Currency: r.Pay.Currency,
				Period: r.Pay.Period, Source: r.Pay.Source}
		}
		out = append(out, row)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}
	return nil
}

// sorted returns rows ordered by (board, title, key), case-insensitively —
// deterministic output regardless of worker completion order.
func sorted(rows []store.Row) []store.Row {
	out := make([]store.Row, len(rows))
	copy(out, rows)
	sort.SliceStable(out, func(i, j int) bool {
		bi, bj := strings.ToLower(out[i].Board), strings.ToLower(out[j].Board)
		if bi != bj {
			return bi < bj
		}
		ti, tj := strings.ToLower(out[i].Title), strings.ToLower(out[j].Title)
		if ti != tj {
			return ti < tj
		}
		return out[i].Key() < out[j].Key()
	})
	return out
}

func published(r store.Row) string {
	if r.PublishedAt.IsZero() {
		return ""
	}
	return r.PublishedAt.UTC().Format(time.RFC3339)
}

package export

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/joelrmartinez/connie/ats"
	"github.com/joelrmartinez/connie/internal/store"
)

func rows() []store.Row {
	published := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	return []store.Row{
		{Job: ats.Job{ATS: "lever", Board: "zeta", ExternalID: "2", Title: "ZZ Ops", URL: "https://z/2"}},
		{Job: ats.Job{ATS: "ashby", Board: "acme", ExternalID: "1", Title: "beta Engineer",
			Location: "Remote", Remote: true, URL: "https://a/1", PublishedAt: published}, Matched: "ctx"},
		{Job: ats.Job{ATS: "ashby", Board: "acme", ExternalID: "3", Title: "Alpha Engineer", URL: "https://a/3"}},
	}
}

func TestCSV(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := CSV(&buf, rows()); err != nil {
		t.Fatalf("CSV: %v", err)
	}
	out := buf.String()

	// Excel needs the UTF-8 BOM.
	if !strings.HasPrefix(out, "\uFEFF") {
		t.Fatal("CSV missing UTF-8 BOM")
	}

	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(out, "\uFEFF")))
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("parse CSV back: %v", err)
	}
	wantHeader := []string{"ats", "board", "external_id", "title", "location",
		"department", "team", "employment_type", "remote", "url", "published_at", "matched"}
	if strings.Join(records[0], ",") != strings.Join(wantHeader, ",") {
		t.Fatalf("header = %v, want %v", records[0], wantHeader)
	}
	if len(records) != 4 {
		t.Fatalf("rows = %d, want 4 (header + 3)", len(records))
	}
	// Deterministic sort: board asc, then title asc case-insensitively.
	if records[1][3] != "Alpha Engineer" || records[2][3] != "beta Engineer" || records[3][3] != "ZZ Ops" {
		t.Fatalf("sort order wrong: %v / %v / %v", records[1][3], records[2][3], records[3][3])
	}
	// Field spot checks on the fully-populated row.
	beta := records[2]
	if beta[8] != "true" || beta[10] != "2026-07-01T00:00:00Z" || beta[11] != "ctx" {
		t.Fatalf("row fields wrong: %v", beta)
	}
	// Zero published_at renders empty, not the zero time.
	if records[1][10] != "" {
		t.Fatalf("zero published_at = %q, want empty", records[1][10])
	}
}

func TestCSVEmptyIsHeaderOnly(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := CSV(&buf, nil); err != nil {
		t.Fatalf("CSV: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(strings.TrimPrefix(buf.String(), "\uFEFF")), "\n")
	if len(lines) != 1 {
		t.Fatalf("empty export = %d lines, want header only", len(lines))
	}
}

func TestJSON(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := JSON(&buf, rows()); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("parse JSON back: %v", err)
	}
	if len(decoded) != 3 {
		t.Fatalf("decoded %d rows, want 3", len(decoded))
	}
	if decoded[0]["title"] != "Alpha Engineer" {
		t.Fatalf("JSON not sorted: first = %v", decoded[0]["title"])
	}
	if decoded[1]["remote"] != true || decoded[1]["matched"] != "ctx" {
		t.Fatalf("JSON fields wrong: %v", decoded[1])
	}
}

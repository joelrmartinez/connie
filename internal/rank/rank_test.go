package rank

import (
	"strings"
	"testing"

	"github.com/joelrmartinez/connie/ats"
	"github.com/joelrmartinez/connie/internal/config"
)

func cfg() *config.Config {
	c := config.Defaults()
	c.Pay.Floor = 150000
	return c
}

func TestScore(t *testing.T) {
	field := ats.Pay{Min: 160000, Max: 200000, Currency: "USD", Period: ats.PerYear, Source: ats.PayFromField}
	low := ats.Pay{Min: 100000, Max: 120000, Currency: "USD", Period: ats.PerYear, Source: ats.PayFromField}
	desc := ats.Pay{Min: 170000, Max: 190000, Currency: "USD", Period: ats.PerYear, Source: ats.PayFromDescription}
	tests := []struct {
		name   string
		job    ats.Job
		count  bool
		total  float64
		reason string
	}{
		{"perfect", ats.Job{Title: "Senior GTM Engineer", Remote: true, Location: "Remote - US", Pay: field},
			false, 100, "title GTM Engineer, pay USD 160k-200k/year (field), remote US"},
		{"unknown pay", ats.Job{Title: "GTM Engineer", Location: "Seattle, WA"},
			false, 85, "title GTM Engineer, pay unknown, Seattle hybrid/onsite"},
		{"below floor", ats.Job{Title: "RevOps Engineer", Remote: true, Location: "United States", Pay: low},
			false, 92, "title Revenue Engineer, pay USD 100k-120k/year (field) below 150k floor, remote US"},
		{"description pay not counted", ats.Job{Title: "Growth Engineer", Remote: true, Pay: desc},
			false, 80, "pay unknown (description pay not counted)"},
		{"description pay counted", ats.Job{Title: "Growth Engineer", Remote: true, Pay: desc},
			true, 95, "(description)"},
		{"partial title onsite", ats.Job{Title: "Engineer, Growth", Location: "Austin, TX"},
			false, 40, "title partial Growth Engineer, pay unknown, onsite Austin, TX"},
		{"no title", ats.Job{Title: "Account Executive", Remote: true, Location: "US"},
			false, 35, "title no match"},
	}
	for _, tt := range tests {
		c := cfg()
		c.Pay.CountDescription = tt.count
		got := New(c).Score(tt.job)
		if got.Total != tt.total || !strings.Contains(got.Reason, tt.reason) {
			t.Errorf("%s: got %.1f %q, want %.1f containing %q", tt.name, got.Total, got.Reason, tt.total, tt.reason)
		}
	}
}

func TestWeights(t *testing.T) {
	c := cfg()
	c.Weights = config.Weights{Title: 0, Pay: 0, Location: 10}
	if got := New(c).Score(ats.Job{Title: "Account Executive", Remote: true, Location: "USA"}).Total; got != 10 {
		t.Errorf("location-only weight = %.1f, want 10", got)
	}
	c.Weights = config.Weights{}
	if got := New(c).Score(ats.Job{Title: "GTM Engineer", Remote: true, Location: "USA"}).Total; got != 85 {
		t.Errorf("zero weights fall back to defaults: got %.1f, want 85", got)
	}
}

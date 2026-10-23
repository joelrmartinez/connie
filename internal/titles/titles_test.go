package titles

import (
	"testing"

	"github.com/joelrmartinez/connie/internal/config"
)

func TestNormalize(t *testing.T) {
	tests := map[string]string{
		"Sr. GTM Engineer (Remote)":              "go to market engineer",
		"Go-To-Market Engineer - San Francisco":  "go to market engineer",
		"Staff Growth Eng, Platform":             "growth engineer platform",
		"RevOps Engineer | Remote US":            "revenue operations engineer",
		"Rev Ops Engineer II":                    "revenue operations engineer",
		"Sales & Marketing Systems Mgr":          "sales and marketing systems manager",
		"Founding GTM Engineer \u2013 Clay, n8n": "go to market engineer",
		"Engineer":                               "engineer",
		"":                                       "",
	}
	for in, want := range tests {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClassifyDefaultFamilies(t *testing.T) {
	c := config.Defaults()
	tab := New(c.Titles, c.Variants)
	tests := map[string]string{
		"Senior GTM Engineer":                         "GTM Engineer",
		"Go-To-Market Engineer (Remote, US)":          "GTM Engineer",
		"GTM Engineering Lead":                        "GTM Engineer",
		"GTM Systems Engineer":                        "GTM Systems",
		"Sales Systems Administrator":                 "GTM Systems",
		"Business Systems Analyst, GTM":               "GTM Systems",
		"Revenue Engineer":                            "Revenue Engineer",
		"RevOps Engineer":                             "Revenue Engineer",
		"Senior Revenue Operations Engineer - Remote": "Revenue Engineer",
		"Revenue Systems Manager":                     "Revenue Engineer",
		"Growth Engineer, Activation":                 "Growth Engineer",
		"Product Growth Engineer":                     "Growth Engineer",
		"Staff Growth Engineering Lead":               "Growth Engineer",
	}
	for in, want := range tests {
		if got, ok := tab.Classify(in); !ok || got != want {
			t.Errorf("Classify(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{
		"Revenue Accountant",
		"Engineering Manager",
		"Software Engineer",
		"Growth Marketing Manager",
		"Account Executive",
		"Revenue Operations Manager",
		"",
	} {
		if got, ok := tab.Classify(in); ok {
			t.Errorf("Classify(%q) = %q, want no family", in, got)
		}
	}
}

// Users edit variants in the config; a new variant takes effect, and the
// family name is matched case-insensitively against the variants map.
func TestClassifyEditedVariants(t *testing.T) {
	tab := New([]string{"Solutions Engineer"}, map[string][]string{
		"solutions engineer": {"sales engineer", "Pre-Sales Engineer"},
	})
	for _, in := range []string{"Senior Sales Engineer", "Presales Engineer", "Pre-Sales Engineer, EMEA", "Solutions Engineer"} {
		_, ok := tab.Classify(in)
		if want := in != "Presales Engineer"; ok != want {
			t.Errorf("Classify(%q) ok = %v, want %v", in, ok, want)
		}
	}
	if !New(nil, nil).Empty() || tab.Empty() {
		t.Error("Empty wrong")
	}
}

func TestLongestVariantWins(t *testing.T) {
	tab := New([]string{"Ops", "RevOps Engineer"}, map[string][]string{"Ops": {"revenue operations"}})
	if got, _ := tab.Classify("Revenue Operations Engineer"); got != "RevOps Engineer" {
		t.Errorf("got %q", got)
	}
}

func TestFit(t *testing.T) {
	c := config.Defaults()
	tab := New(c.Titles, c.Variants)
	tests := []struct {
		in     string
		family string
		score  float64
	}{
		{"GTM Engineer", "GTM Engineer", 1},
		{"Engineer, Growth", "Growth Engineer", 0.5},
		{"Systems Engineer, Revenue Platform", "Revenue Engineer", 0.5},
		{"Revenue Operations Manager", "", 0},
		{"Growth Marketing Manager", "", 0},
		{"Sales Manager", "", 0},
		{"Software Engineer", "", 0},
		{"Account Executive", "", 0},
	}
	for _, tt := range tests {
		f, s := tab.Fit(tt.in)
		if f != tt.family || s != tt.score {
			t.Errorf("Fit(%q) = %q %.2f, want %q %.2f", tt.in, f, s, tt.family, tt.score)
		}
	}
}

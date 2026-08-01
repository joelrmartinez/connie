package ats

import "testing"

func TestPlausibleSlug(t *testing.T) {
	t.Parallel()
	tests := []struct {
		slug        string
		allowSpaces bool
		want        bool
	}{
		// real slugs survive
		{"ramp", false, true},
		{"pareto-ai", false, true},
		{"titan_ai", false, true},
		{"A1 Garage Door", true, true}, // Ashby allows spaces
		{"middesk", false, true},

		// archive noise dies
		{"", false, false},
		{"A1 Garage Door", false, false}, // spaces rejected where not allowed
		{"_next", false, false},          // leading underscore fails shape
		{"api", false, false},            // junk word
		{"embed", false, false},
		{"static", false, false},
		{"favicon.ico", false, false},
		{"root.3fa85f64-5717-4562-b3fc-2c963f66afa6", false, false}, // root.<uuid>
		{"deadbeefdeadbeefdeadbeef", false, false},                  // hex tracking blob
		{"3fa85f6457174562b3fc2c963f66afa6", false, false},          // bare uuid hex
		{"verylongbutnothex-and-fine", false, true},
		{"x", false, true}, // short but legitimate
	}
	for _, tt := range tests {
		if got := PlausibleSlug(tt.slug, tt.allowSpaces); got != tt.want {
			t.Errorf("PlausibleSlug(%q, spaces=%v) = %v, want %v", tt.slug, tt.allowSpaces, got, tt.want)
		}
	}
}

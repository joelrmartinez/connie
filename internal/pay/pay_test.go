package pay

import (
	"testing"

	"github.com/joelrmartinez/connie/ats"
)

func TestParse(t *testing.T) {
	d := ats.PayFromDescription
	tests := []struct {
		text string
		want ats.Pay
	}{
		{"The annual OTE range for this role is $80,000-$165,000. You may also...",
			ats.Pay{Min: 80000, Max: 165000, Currency: "USD", Period: ats.PerYear, Source: d}},
		{"Salary: $211.4K \u2013 $290.6K \u2022 Offers Equity",
			ats.Pay{Min: 211400, Max: 290600, Currency: "USD", Period: ats.PerYear, Source: d}},
		{"Base pay $150k to $180k USD per year plus equity",
			ats.Pay{Min: 150000, Max: 180000, Currency: "USD", Period: ats.PerYear, Source: d}},
		{"Range: $150-180k", ats.Pay{Min: 150000, Max: 180000, Currency: "USD", Period: ats.PerYear, Source: d}},
		{"We pay USD 120,000 \u2014 140,000 annually.",
			ats.Pay{Min: 120000, Max: 140000, Currency: "USD", Period: ats.PerYear, Source: d}},
		{"Hourly rate: $45 - $60 per hour", ats.Pay{Min: 45, Max: 60, Currency: "USD", Period: ats.PerHour, Source: d}},
		{"\u00a355,000 - \u00a370,000 per annum", ats.Pay{Min: 55000, Max: 70000, Currency: "GBP", Period: ats.PerYear, Source: d}},
		{"Compensation: \u20ac6.000 per month", ats.Pay{}}, // European thousands dot is ambiguous; skip
		{"Base salary of $175,000", ats.Pay{Min: 175000, Max: 175000, Currency: "USD", Period: ats.PerYear, Source: d}},
		{"Starting at $32/hr", ats.Pay{Min: 32, Max: 32, Currency: "USD", Period: ats.PerHour, Source: d}},
	}
	for _, tt := range tests {
		got, ok := Parse(tt.text)
		if ok != tt.want.Known() || got != tt.want {
			t.Errorf("Parse(%q) = %+v, %v; want %+v", tt.text, got, ok, tt.want)
		}
	}
}

// Text that has money-looking numbers but no pay must stay unknown.
func TestParseRejectsNonPay(t *testing.T) {
	for _, text := range []string{
		"",
		"We raised $50M in our Series B from top investors.",
		"Generous 401(k) match and 20 days PTO.",
		"Teams of 10-20 engineers ship weekly.",
		"Manage a $2,000,000 - $5,000,000 annual budget.",
		"Customers range from 5,000 to 50,000 employees.",
		"Our ARR grew from $1M to $10M.",
	} {
		if p, ok := Parse(text); ok {
			t.Errorf("Parse(%q) = %+v, want unknown", text, p)
		}
	}
}

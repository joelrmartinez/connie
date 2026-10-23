package ats

import "testing"

func TestPayString(t *testing.T) {
	tests := []struct {
		p    Pay
		want string
	}{
		{Pay{}, "pay unknown"},
		{Pay{Min: 150000, Max: 180000, Currency: "USD", Period: PerYear, Source: PayFromField}, "USD 150k-180k/year (field)"},
		{Pay{Min: 211400, Max: 290600, Currency: "USD", Period: PerYear, Source: PayFromField}, "USD 211.4k-290.6k/year (field)"},
		{Pay{Max: 45, Currency: "USD", Period: PerHour, Source: PayFromDescription}, "USD 45/hour (description)"},
	}
	for _, tt := range tests {
		if got := tt.p.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}

func TestPayAnnual(t *testing.T) {
	if got := (Pay{Min: 100000, Max: 200000, Period: PerYear, Source: PayFromField}).Annual(); got != 150000 {
		t.Errorf("annual = %v", got)
	}
	if got := (Pay{Min: 50, Period: PerHour, Source: PayFromField}).Annual(); got != 104000 {
		t.Errorf("hourly annual = %v", got)
	}
	if got := (Pay{Max: 10000, Period: PerMonth, Source: PayFromField}).Annual(); got != 120000 {
		t.Errorf("monthly annual = %v", got)
	}
	if (Pay{}).Annual() != 0 {
		t.Error("unknown pay must annualize to 0")
	}
}

func TestPeriodFromText(t *testing.T) {
	for in, want := range map[string]string{
		"1 YEAR": PerYear, "per-year-salary": PerYear, "per-hour-wage": PerHour,
		"1 MONTH": PerMonth, "per-week": PerWeek, "one-time": "", "NONE": "",
	} {
		if got := PeriodFromText(in); got != want {
			t.Errorf("PeriodFromText(%q) = %q, want %q", in, got, want)
		}
	}
}

package place

import (
	"testing"

	"github.com/joelrmartinez/connie/ats"
	"github.com/joelrmartinez/connie/internal/config"
)

func TestJudge(t *testing.T) {
	usSeattle := config.Location{Remote: config.RemoteUS, Cities: []string{"Seattle"}, Hybrid: true}
	tests := []struct {
		name  string
		job   ats.Job
		loc   config.Location
		score float64
	}{
		{"remote US", ats.Job{Remote: true, Location: "Remote - United States"}, usSeattle, 1},
		{"remote USA short", ats.Job{Remote: true, Location: "Remote (USA)"}, usSeattle, 1},
		{"remote no region", ats.Job{Remote: true, Location: "Remote"}, usSeattle, 0.75},
		{"remote Europe", ats.Job{Remote: true, Location: "Remote, Europe"}, usSeattle, 0.2},
		{"remote anywhere scope", ats.Job{Remote: true, Location: "Remote, UK"}, config.Location{Remote: config.RemoteAnywhere}, 1},
		{"remote off", ats.Job{Remote: true, Location: "Remote - US"}, config.Location{Remote: config.RemoteOff}, 0},
		{"seattle hybrid", ats.Job{Location: "Seattle, WA"}, usSeattle, 1},
		{"seattle without hybrid", ats.Job{Location: "Seattle, WA"}, config.Location{Remote: config.RemoteUS, Cities: []string{"Seattle"}}, 0},
		{"other city", ats.Job{Location: "Austin, TX"}, usSeattle, 0},
		{"no location", ats.Job{}, usSeattle, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Judge(tt.job, tt.loc)
			if got.Score != tt.score {
				t.Fatalf("Judge = %+v, want score %v", got, tt.score)
			}
			if got.Reason == "" {
				t.Fatal("empty reason")
			}
		})
	}
}

func TestAcceptable(t *testing.T) {
	if !Acceptable(Fit{Score: 0.75}) || Acceptable(Fit{Score: 0.2}) {
		t.Fatal("Acceptable threshold wrong")
	}
}

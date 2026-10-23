// Package place judges whether a posting's location fits a saved search:
// remote within the right region, or an office in a city the user can
// reach. Location strings are free text, so the region test is a set of
// plain markers, and a remote posting that names no region is reported as
// such rather than guessed.
package place

import (
	"regexp"
	"strings"

	"github.com/joelrmartinez/connie/ats"
	"github.com/joelrmartinez/connie/internal/config"
)

var (
	usMarker = regexp.MustCompile(`(?i)\b(us|usa|u\.s\.a?\.?|united states|america|north america|americas)\b`)
	// Regions and countries that commonly mark a remote role as not open
	// to US residents. Not exhaustive; unknown stays unknown.
	nonUSMarker = regexp.MustCompile(`(?i)\b(emea|apac|latam|europe|eu|uk|united kingdom|england|ireland|germany|france|spain|portugal|netherlands|poland|italy|sweden|canada|mexico|brazil|argentina|colombia|india|singapore|japan|australia|new zealand|israel|philippines|south africa|nigeria|kenya)\b`)
)

// Fit is a location verdict: Score in [0,1] and a short reason.
type Fit struct {
	Score  float64
	Reason string
}

// Judge rates j against loc.
func Judge(j ats.Job, loc config.Location) Fit {
	where := strings.TrimSpace(j.Location)
	if j.Remote && loc.Remote != config.RemoteOff {
		if loc.Remote == config.RemoteAnywhere {
			return Fit{1, "remote"}
		}
		us := usMarker.MatchString(where)
		nonUS := nonUSMarker.MatchString(where)
		switch {
		case us:
			return Fit{1, "remote US"}
		case nonUS:
			return Fit{0.2, "remote outside US"}
		default:
			return Fit{0.75, "remote, region unstated"}
		}
	}
	if loc.Hybrid {
		low := strings.ToLower(where)
		for _, c := range loc.Cities {
			c = strings.TrimSpace(c)
			if c != "" && strings.Contains(low, strings.ToLower(c)) {
				return Fit{1, c + " hybrid/onsite"}
			}
		}
	}
	if j.Remote {
		return Fit{0, "remote (not wanted)"}
	}
	if where == "" {
		return Fit{0, "location unstated"}
	}
	return Fit{0, "onsite " + where}
}

// Acceptable is the hard-filter reading of Judge: a fit good enough to
// keep when results are filtered rather than ranked.
func Acceptable(f Fit) bool { return f.Score >= 0.5 }

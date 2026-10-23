// Package rank scores postings against a saved search so the best fits
// come first instead of everything else being thrown away.
package rank

import (
	"fmt"
	"math"
	"strings"

	"github.com/joelrmartinez/connie/ats"
	"github.com/joelrmartinez/connie/internal/config"
	"github.com/joelrmartinez/connie/internal/place"
	"github.com/joelrmartinez/connie/internal/titles"
)

// Ranker scores jobs for one config.
type Ranker struct {
	cfg      *config.Config
	families *titles.Table
}

// New builds a Ranker. Weights default when the config sets none.
func New(cfg *config.Config) *Ranker {
	return &Ranker{cfg: cfg, families: titles.New(cfg.Titles, cfg.Variants)}
}

// Score is one job's ranking.
type Score struct {
	Total  float64 // 0 to the sum of the weights, one decimal
	Family string  // best title family, "" when none
	Title  float64 // title fit, 0 to 1; 0 means drop
	Reason string  // e.g. "title GTM Engineer, pay unknown, remote US"
}

// Score rates j. Each signal is 0 to 1 and multiplied by its weight.
// Unknown pay scores half so it neither wins nor sinks a posting.
func (r *Ranker) Score(j ats.Job) Score {
	w := r.cfg.Weights
	if w == (config.Weights{}) {
		w = config.DefaultWeights
	}
	fam, tf := r.families.Fit(j.Title)
	ps, preason := r.pay(j.Pay)
	loc := place.Judge(j, r.cfg.Location)

	treason := "title " + fam
	if tf == 0 {
		treason = "title no match"
	} else if tf < 1 {
		treason = "title partial " + fam
	}
	total := tf*float64(w.Title) + ps*float64(w.Pay) + loc.Score*float64(w.Location)
	return Score{
		Total:  math.Round(total*10) / 10,
		Family: fam,
		Title:  tf,
		Reason: strings.Join([]string{treason, preason, loc.Reason}, ", "),
	}
}

func (r *Ranker) pay(p ats.Pay) (float64, string) {
	if p.Source == ats.PayFromDescription && !r.cfg.Pay.CountDescription {
		return 0.5, "pay unknown (description pay not counted)"
	}
	if !p.Known() {
		return 0.5, "pay unknown"
	}
	floor := float64(r.cfg.Pay.Floor)
	a := p.Annual()
	switch {
	case floor == 0 || a >= floor:
		return 1, "pay " + p.String()
	case a <= 0:
		return 0.5, "pay " + p.String()
	default:
		return a / floor, fmt.Sprintf("pay %s below %s floor", p.String(), short(floor))
	}
}

func short(v float64) string { return fmt.Sprintf("%.0fk", v/1000) }

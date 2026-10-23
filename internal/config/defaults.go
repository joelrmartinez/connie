package config

import "strings"

// KnownVariants holds the other names common families go by. connie init
// copies the matching entries into a new file so users start with them and
// edit from there.
var KnownVariants = map[string][]string{
	"GTM Engineer":       {"go-to-market engineer", "gtm engineering", "gtm automation engineer"},
	"GTM Systems":        {"gtm systems engineer", "sales systems", "marketing systems", "business systems"},
	"Revenue Engineer":   {"revops engineer", "revenue operations engineer", "revenue systems"},
	"Growth Engineer":    {"growth engineering", "product growth engineer"},
	"Account Executive":  {"enterprise account executive", "mid-market account executive"},
	"Solutions Engineer": {"sales engineer", "solutions consultant", "pre-sales engineer"},
}

// AllATS is every adapter connie ships, in --ats order.
var AllATS = []string{"ashby", "greenhouse", "lever", "workable"}

// Defaults is the starting search connie init offers: US remote plus
// Seattle hybrid, description pay counts, no pay floor (pay ranks, it
// never cuts), GTM engineering titles.
func Defaults() *Config {
	c := &Config{
		Titles:   []string{"GTM Engineer", "GTM Systems", "Revenue Engineer", "Growth Engineer"},
		Location: Location{Remote: RemoteUS, Cities: []string{"Seattle"}, Hybrid: true},
		Pay:      Pay{CountDescription: true},
		Weights:  DefaultWeights,
		ATS:      append([]string(nil), AllATS...),
	}
	c.Variants = VariantsFor(c.Titles)
	return c
}

// VariantsFor returns the known variants for titles, matched
// case-insensitively. Titles with no known variants are left out.
func VariantsFor(titles []string) map[string][]string {
	out := map[string][]string{}
	for _, t := range titles {
		for fam, vs := range KnownVariants {
			if strings.EqualFold(strings.TrimSpace(t), fam) {
				out[t] = append([]string(nil), vs...)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

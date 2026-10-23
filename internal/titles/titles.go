// Package titles maps raw job titles to the families a saved search asks
// for. A family is a canonical name ("GTM Engineer") plus the variants it
// is posted under ("go-to-market engineer", "gtm engineering"), all read
// from the config file so users can edit them.
//
// Titles and variants go through the same Normalize, and a variant
// matches when its words appear as a run inside the title's words. So
// "Sr. GTM Engineer (Remote)" and "Go-To-Market Engineer - Platform" both
// land in "GTM Engineer", while "Engineering Manager" does not match
// "engineer".
package titles

import (
	"regexp"
	"strings"
)

var (
	// Tails that carry location or team, not the role: anything after a
	// spaced hyphen, en dash, em dash, or pipe, and parentheticals.
	tail  = regexp.MustCompile(`\s+(?:-|\x{2013}|\x{2014}|\|)\s+.*$|\s*\|.*$`)
	paren = regexp.MustCompile(`\([^)]*\)|\[[^\]]*\]`)
	// Anything that is not a letter or digit separates words.
	nonWord = regexp.MustCompile(`[^\p{L}\p{N}]+`)
)

// expansions rewrite one word into canonical words.
var expansions = map[string]string{
	"gtm":      "go to market",
	"revops":   "revenue operations",
	"salesops": "sales operations",
	"ops":      "operations",
	"eng":      "engineer",
	"engr":     "engineer",
	"mgr":      "manager",
	"sys":      "systems",
	"swe":      "software engineer",
	"sde":      "software engineer",
	"bizops":   "business operations",
}

// levels are seniority words dropped before matching.
var levels = map[string]bool{
	"senior": true, "sr": true, "staff": true, "principal": true, "lead": true,
	"junior": true, "jr": true, "associate": true, "founding": true,
	"i": true, "ii": true, "iii": true, "iv": true, "1": true, "2": true, "3": true,
}

// Normalize lowercases a title, drops location tails, parentheticals and
// level words, and expands common abbreviations. The result is
// space-separated words.
func Normalize(raw string) string {
	s := strings.ToLower(raw)
	s = tail.ReplaceAllString(s, "")
	s = paren.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "&", " and ")
	s = strings.ReplaceAll(s, "go-to-market", "go to market")
	var out []string
	for _, w := range nonWord.Split(s, -1) {
		if w == "" || levels[w] {
			continue
		}
		if e, ok := expansions[w]; ok {
			w = e
		}
		out = append(out, w)
	}
	// "rev ops" spelled as two words.
	return strings.ReplaceAll(strings.Join(out, " "), "rev operations", "revenue operations")
}

// Table is a set of families built once from config.
type Table struct {
	families []family
}

type family struct {
	name     string
	variants [][]string // normalized, split into words
}

// New builds a Table. Each title is a family; its own name always counts
// as a variant, plus any listed under that name in variants (matched
// case-insensitively).
func New(families []string, variants map[string][]string) *Table {
	byLower := map[string][]string{}
	for k, v := range variants {
		byLower[strings.ToLower(strings.TrimSpace(k))] = append(byLower[strings.ToLower(strings.TrimSpace(k))], v...)
	}
	t := &Table{}
	for _, name := range families {
		f := family{name: name}
		for _, v := range append([]string{name}, byLower[strings.ToLower(strings.TrimSpace(name))]...) {
			if words := strings.Fields(Normalize(v)); len(words) > 0 {
				f.variants = append(f.variants, words)
			}
		}
		if len(f.variants) > 0 {
			t.families = append(t.families, f)
		}
	}
	return t
}

// Empty reports whether the table has no families.
func (t *Table) Empty() bool { return len(t.families) == 0 }

// Classify returns the family for a raw title. When several variants
// match, the longest wins, so "revenue operations engineer" beats a bare
// "revenue operations".
func (t *Table) Classify(raw string) (string, bool) {
	words := strings.Fields(Normalize(raw))
	best, bestLen := "", 0
	for _, f := range t.families {
		for _, v := range f.variants {
			if len(v) > bestLen && containsRun(words, v) {
				best, bestLen = f.name, len(v)
			}
		}
	}
	return best, bestLen > 0
}

// containsRun reports whether needle appears as consecutive words in hay.
func containsRun(hay, needle []string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		ok := true
		for j, w := range needle {
			if hay[i+j] != w {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

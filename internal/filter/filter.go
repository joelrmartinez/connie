// Package filter decides which postings the user cares about: fuzzy/exact
// title matching, case-insensitive regex over descriptions with context
// fragments, and a remote-only gate. Pure functions — no I/O, no state.
//
// The matching semantics are ported from the Python reference and are
// deliberate (each pinned by a test):
//   - fuzzy matches in both directions, but the reverse direction requires a
//     multi-word title so "senior software engineer" can't match every job
//     titled just "Engineer";
//   - an empty query matches nothing (it would otherwise match everything);
//   - grep searches descriptions, not titles.
package filter

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/joelrmartinez/connie/ats"
)

var (
	htmlTag = regexp.MustCompile(`<[^>]+>`)
	// \s in Go is ASCII-only; \p{Zs} catches the U+00A0 that &nbsp; decodes
	// to (Python's \s matches it, so the reference behavior needs the union).
	whitespace = regexp.MustCompile(`[\s\p{Zs}]+`)
)

const (
	fragmentBefore = 90
	fragmentAfter  = 150
	fragmentLimit  = 2
)

// Filter is an immutable set of match criteria, built once from flags.
type Filter struct {
	title      string
	exact      bool
	grep       *regexp.Regexp
	remoteOnly bool
}

// Option configures a Filter.
type Option func(*Filter) error

// WithTitle matches job titles against query — fuzzy by default, exact when
// exact is true.
func WithTitle(query string, exact bool) Option {
	return func(f *Filter) error {
		f.title = strings.TrimSpace(query)
		f.exact = exact
		return nil
	}
}

// WithGrep matches a case-insensitive regex against posting descriptions.
func WithGrep(pattern string) Option {
	return func(f *Filter) error {
		re, err := regexp.Compile("(?i)" + pattern)
		if err != nil {
			return fmt.Errorf("compile grep pattern %q: %w", pattern, err)
		}
		f.grep = re
		return nil
	}
}

// WithRemoteOnly drops postings not marked remote.
func WithRemoteOnly() Option {
	return func(f *Filter) error {
		f.remoteOnly = true
		return nil
	}
}

// New builds a Filter, surfacing bad patterns at construction rather than
// mid-scan.
func New(opts ...Option) (*Filter, error) {
	f := &Filter{}
	for _, opt := range opts {
		if err := opt(f); err != nil {
			return nil, err
		}
	}
	return f, nil
}

// Match reports whether the job passes every configured criterion. When a
// grep pattern is set, fragments carries up to two deduped context windows
// around the matches — enough to judge a hit without keeping the full text.
func (f *Filter) Match(j ats.Job) (ok bool, fragments []string) {
	if f.remoteOnly && !j.Remote {
		return false, nil
	}
	if f.title != "" && !TitleMatches(j.Title, f.title, f.exact) {
		return false, nil
	}
	if f.grep != nil {
		text := PlainText(j.Description)
		fragments = fragmentsOf(text, f.grep)
		if len(fragments) == 0 {
			return false, nil
		}
	}
	return true, fragments
}

// NeedsDescriptions reports whether adapters must retain description text.
func (f *Filter) NeedsDescriptions() bool { return f.grep != nil }

// Unfiltered reports whether this filter passes every listed job — the
// precondition for a scan to be allowed to close vanished postings.
func (f *Filter) Unfiltered() bool {
	return f.title == "" && f.grep == nil && !f.remoteOnly
}

// TitleMatches implements the reference title semantics. Case-insensitive.
func TitleMatches(title, query string, exact bool) bool {
	t := strings.ToLower(strings.TrimSpace(title))
	q := strings.ToLower(strings.TrimSpace(query))
	if t == "" || q == "" {
		return false
	}
	if exact {
		return t == q
	}
	return strings.Contains(t, q) ||
		(len(strings.Fields(t)) >= 2 && strings.Contains(q, t))
}

// PlainText strips HTML tags, decodes entities, and collapses whitespace.
func PlainText(value string) string {
	return strings.TrimSpace(whitespace.ReplaceAllString(
		html.UnescapeString(htmlTag.ReplaceAllString(value, " ")), " "))
}

// fragmentsOf returns up to fragmentLimit deduped context windows around
// pattern matches in text.
func fragmentsOf(text string, pattern *regexp.Regexp) []string {
	var found []string
	for _, loc := range pattern.FindAllStringIndex(text, -1) {
		start := max(0, loc[0]-fragmentBefore)
		end := min(len(text), loc[1]+fragmentAfter)
		window := strings.TrimSpace(text[start:end])
		if !contains(found, window) {
			found = append(found, window)
		}
		if len(found) == fragmentLimit {
			break
		}
	}
	return found
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

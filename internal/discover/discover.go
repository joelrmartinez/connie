// Package discover finds board slugs in the Wayback Machine's CDX index and
// validates them against the live ATS.
//
// Two-file discipline (ported from the reference): a small curated seed file
// is committed so a fresh clone works without crawling; the full discovery
// cache is gitignored, so a crawl never turns the repo into a published
// customer list. Union semantics never lose a board — seed ∪ cache ∪ live.
package discover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joelrmartinez/connie/ats"
)

// DefaultCDX is the Wayback Machine's CDX search endpoint.
const DefaultCDX = "https://web.archive.org/cdx/search/cdx"

// Config tunes a harvest.
type Config struct {
	// Concurrency caps in-flight probes; default 8.
	Concurrency int
	// CDXBase overrides the CDX endpoint (tests); default DefaultCDX.
	CDXBase string
	// Progress, when set, is called after each candidate is probed.
	Progress func(done, total int)
}

// Harvest queries the CDX index for every host the Discoverer claims,
// extracts candidate slugs, drops implausible ones, dedupes
// case-insensitively (first casing wins), probes the survivors against the
// live ATS, and returns the sorted slugs that answered.
func Harvest(ctx context.Context, d ats.Discoverer, client ats.Getter, cfg Config) ([]string, error) {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 8
	}
	if cfg.CDXBase == "" {
		cfg.CDXBase = DefaultCDX
	}

	// Phase 1: harvest candidates from the archive.
	var candidates []string
	seen := map[string]bool{}
	for _, target := range targetsFor(d) {
		cdxURL := fmt.Sprintf("%s?url=%s&matchType=%s&fl=original&collapse=urlkey&output=json",
			cfg.CDXBase, url.QueryEscape(target.URL), url.QueryEscape(target.MatchType))
		body, err := client.Get(ctx, cdxURL)
		if err != nil {
			return nil, fmt.Errorf("query CDX for %s: %w", target.URL, err)
		}

		// output=json is an array of one-column rows, header first.
		var rows [][]string
		if err := json.Unmarshal(body, &rows); err != nil {
			return nil, fmt.Errorf("parse CDX response for %s: %w", target.URL, err)
		}
		for i, row := range rows {
			if i == 0 || len(row) == 0 { // header row
				continue
			}
			slug, ok := d.SlugFromURL(row[0])
			if !ok || !d.Plausible(slug) {
				continue
			}
			key := strings.ToLower(slug)
			if seen[key] {
				continue
			}
			seen[key] = true
			candidates = append(candidates, slug)
		}
	}

	// Phase 2: probe candidates against the live ATS, bounded.
	type verdict struct {
		slug string
		live bool
		err  error
	}
	feed := make(chan string)
	verdicts := make(chan verdict)
	go func() {
		defer close(feed)
		for _, c := range candidates {
			select {
			case feed <- c:
			case <-ctx.Done():
				return
			}
		}
	}()
	done := make(chan struct{})
	for i := 0; i < cfg.Concurrency; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for slug := range feed {
				live, err := d.Probe(ctx, slug)
				verdicts <- verdict{slug: slug, live: live, err: err}
			}
		}()
	}
	go func() {
		for i := 0; i < cfg.Concurrency; i++ {
			<-done
		}
		close(verdicts)
	}()

	var live []string
	completed := 0
	for v := range verdicts {
		completed++
		// Probe errors are tolerated: an unreachable board today may answer
		// tomorrow, and union semantics mean nothing is lost either way.
		if v.err == nil && v.live {
			live = append(live, v.slug)
		}
		if cfg.Progress != nil {
			cfg.Progress(completed, len(candidates))
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("harvest interrupted: %w", err)
	}

	sort.Strings(live)
	return live, nil
}

// Union merges slug lists, deduping case-insensitively with the first
// casing winning, sorted. It never shrinks: every input slug survives.
func Union(lists ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range lists {
		for _, s := range list {
			key := strings.ToLower(s)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// Load reads a board file. A missing file is an empty list, not an error —
// the cache legitimately doesn't exist before the first discovery.
func Load(path string) ([]string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read boards %q: %w", path, err)
	}
	var boards []string
	if err := json.Unmarshal(body, &boards); err != nil {
		return nil, fmt.Errorf("parse boards %q: %w", path, err)
	}
	return boards, nil
}

// Save writes a board file, creating parent directories as needed.
func Save(path string, boards []string) error {
	body, err := json.MarshalIndent(boards, "", "  ")
	if err != nil {
		return fmt.Errorf("encode boards: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create boards dir: %w", err)
	}
	return writeFile(path, append(body, '\n'))
}

func writeFile(path string, body []byte) error {
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	return nil
}

// targetsFor asks the adapter for explicit archive queries and falls back to
// a domain-wide query per host. The fallback is right for a small footprint;
// the override exists because Greenhouse's is not small.
func targetsFor(d ats.Discoverer) []ats.CDXTarget {
	if t, ok := d.(ats.CDXTargeter); ok {
		if targets := t.CDXTargets(); len(targets) > 0 {
			return targets
		}
	}
	out := make([]ats.CDXTarget, 0, len(d.Hosts()))
	for _, h := range d.Hosts() {
		out = append(out, ats.CDXTarget{URL: h, MatchType: "domain"})
	}
	return out
}

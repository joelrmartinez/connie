package greenhouse

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/joelrmartinez/connie/ats"
	"github.com/joelrmartinez/connie/internal/httpx"
)

// Hosts implements ats.Discoverer. Greenhouse migrated hosts at some point;
// the archive holds both.
func (a *Adapter) Hosts() []string {
	return []string{"boards.greenhouse.io", "job-boards.greenhouse.io"}
}

// SlugFromURL implements ats.Discoverer.
func (a *Adapter) SlugFromURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if host != "boards.greenhouse.io" && host != "job-boards.greenhouse.io" {
		return "", false
	}
	// The embed form: companies iframe their board instead of linking it, so
	// the path is a constant and the slug rides in ?for=. Reading the first
	// path segment here returns "embed", which is a plausible-looking single
	// token, so it would pass the shape filter and quietly pollute the board
	// list while the real slug went unseen. The archive holds a great many of
	// these.
	seg := strings.SplitN(strings.TrimPrefix(u.EscapedPath(), "/"), "/", 2)[0]
	if seg == "embed" {
		for_ := strings.TrimSpace(u.Query().Get("for"))
		if for_ == "" {
			return "", false
		}
		return for_, true
	}

	slug, err := url.PathUnescape(seg)
	if err != nil || slug == "" {
		return "", false
	}
	return slug, true
}

// Plausible implements ats.Discoverer.
func (a *Adapter) Plausible(slug string) bool {
	return ats.PlausibleSlug(slug, false)
}

// Probe implements ats.Discoverer. Verified live 2026-07-27: the boards API
// honors HEAD with correct 200/404 statuses.
func (a *Adapter) Probe(ctx context.Context, board string) (bool, error) {
	err := a.client.Head(ctx, endpoint+url.PathEscape(board)+"/jobs")
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, httpx.ErrNotFound):
		return false, nil
	default:
		return false, fmt.Errorf("probe greenhouse board %q: %w", board, err)
	}
}

// CDXTargets implements ats.CDXTargeter.
//
// A domain-wide query against boards.greenhouse.io does not complete: the
// archive's footprint for that host is enormous and the request times out
// before the body finishes, so discovery returned nothing at all. Two
// narrower queries do better than one impossible one.
//
// The embed path is the valuable half. Companies iframe their board instead
// of linking it, so ?for= carries the slug, and that one prefix yields
// thousands of distinct boards in about thirty seconds.
func (a *Adapter) CDXTargets() []ats.CDXTarget {
	return []ats.CDXTarget{
		{URL: "boards.greenhouse.io/embed/job_board", MatchType: "prefix"},
		{URL: "job-boards.greenhouse.io", MatchType: "domain"},
	}
}

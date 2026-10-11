package ashby

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/joelrmartinez/connie/ats"
	"github.com/joelrmartinez/connie/internal/httpx"
)

// Hosts implements ats.Discoverer.
func (a *Adapter) Hosts() []string { return []string{"jobs.ashbyhq.com"} }

// SlugFromURL implements ats.Discoverer: the board slug is the first path
// segment, percent-decoded (Ashby slugs may contain spaces).
func (a *Adapter) SlugFromURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Hostname(), "jobs.ashbyhq.com") {
		return "", false
	}
	seg := strings.SplitN(strings.TrimPrefix(u.EscapedPath(), "/"), "/", 2)[0]
	slug, err := url.PathUnescape(seg)
	if err != nil || slug == "" {
		return "", false
	}
	return slug, true
}

// Plausible implements ats.Discoverer.
func (a *Adapter) Plausible(slug string) bool {
	return ats.PlausibleSlug(slug, true) // Ashby allows spaces
}

// Probe implements ats.Discoverer with a zero-body HEAD against the posting
// API. 404 means "no such board" — a fact, not an error.
func (a *Adapter) Probe(ctx context.Context, board string) (bool, error) {
	err := a.client.Head(ctx, endpoint+url.PathEscape(board))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, httpx.ErrNotFound):
		return false, nil
	default:
		return false, fmt.Errorf("probe ashby board %q: %w", board, err)
	}
}

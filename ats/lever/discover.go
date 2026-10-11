package lever

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
func (a *Adapter) Hosts() []string { return []string{"jobs.lever.co"} }

// SlugFromURL implements ats.Discoverer.
func (a *Adapter) SlugFromURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Hostname(), "jobs.lever.co") {
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
	return ats.PlausibleSlug(slug, false)
}

// Probe implements ats.Discoverer. Lever's cheapest liveness check is a
// limit-1 GET — HEAD support is undocumented, and a dead slug 404s with a
// JSON error body (verified live 2026-07-27).
func (a *Adapter) Probe(ctx context.Context, board string) (bool, error) {
	_, err := a.client.Get(ctx, endpoint+url.PathEscape(board)+"?mode=json&limit=1")
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, httpx.ErrNotFound):
		return false, nil
	default:
		return false, fmt.Errorf("probe lever board %q: %w", board, err)
	}
}

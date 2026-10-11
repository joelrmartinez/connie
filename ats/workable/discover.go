package workable

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/joelrmartinez/connie/ats"
	"github.com/joelrmartinez/connie/internal/httpx"
)

// Hosts implements ats.Discoverer. Boards live under apply.workable.com/{slug}
// and, for older accounts, at {slug}.workable.com.
func (a *Adapter) Hosts() []string {
	return []string{"apply.workable.com", "workable.com"}
}

// SlugFromURL implements ats.Discoverer.
func (a *Adapter) SlugFromURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	host := strings.ToLower(u.Hostname())

	// Legacy per-account subdomain: {slug}.workable.com.
	if strings.HasSuffix(host, ".workable.com") && host != "apply.workable.com" {
		slug := strings.TrimSuffix(host, ".workable.com")
		if slug == "" || strings.Contains(slug, ".") {
			return "", false
		}
		return slug, true
	}
	if host != "apply.workable.com" {
		return "", false
	}

	seg := strings.SplitN(strings.TrimPrefix(u.EscapedPath(), "/"), "/", 2)[0]
	slug, err := url.PathUnescape(seg)
	if err != nil || slug == "" {
		return "", false
	}
	// apply.workable.com/j/{shortcode} is a single posting, not a board. Its
	// shortcode is a plausible token, so without this it would be probed as a
	// board and quietly pollute the cache.
	if slug == "j" {
		return "", false
	}
	return slug, true
}

// Plausible implements ats.Discoverer.
func (a *Adapter) Plausible(slug string) bool {
	return ats.PlausibleSlug(slug, false)
}

// Probe implements ats.Discoverer.
func (a *Adapter) Probe(ctx context.Context, board string) (bool, error) {
	err := a.client.Head(ctx, endpoint+url.PathEscape(board))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, httpx.ErrNotFound):
		return false, nil
	default:
		return false, fmt.Errorf("probe workable board %q: %w", board, err)
	}
}

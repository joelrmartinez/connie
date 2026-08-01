package greenhouse

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/saiwrd/connie/ats"
	"github.com/saiwrd/connie/internal/httpx"
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

package ats

import (
	"regexp"
	"strings"
)

// Archive URLs are full of things that look like board slugs and aren't:
// framework asset paths, tracking blobs, bare UUIDs. The shape filter runs
// before any network probe so validation stays cheap.
var (
	slugShape      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,60}$`)
	slugShapeSpace = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,60}$`)
	hexish         = regexp.MustCompile(`^[0-9a-fA-F-]+$`)

	junkSlugs = map[string]bool{
		"api": true, "embed": true, "static": true, "assets": true,
		"jobs": true, "careers": true, "www": true, "app": true,
		"favicon.ico": true, "robots.txt": true, "sitemap.xml": true,
	}
)

// PlausibleSlug reports whether s is shaped like a real board slug.
// allowSpaces matches Ashby, whose slugs may contain spaces ("A1 Garage
// Door"); other ATSes reject them.
func PlausibleSlug(s string, allowSpaces bool) bool {
	shape := slugShape
	if allowSpaces {
		shape = slugShapeSpace
	}
	if !shape.MatchString(s) {
		return false
	}
	lower := strings.ToLower(s)
	if junkSlugs[lower] {
		return false
	}
	if strings.HasPrefix(lower, "root.") {
		return false
	}
	// Long hex runs are tracking ids and UUIDs, not companies.
	if compact := strings.ReplaceAll(s, "-", ""); len(compact) >= 16 && hexish.MatchString(compact) {
		return false
	}
	return true
}

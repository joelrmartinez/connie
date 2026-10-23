// Package seeds embeds the committed seed board lists so the binary runs
// from any directory, not just a checkout.
package seeds

import "embed"

// FS holds every {ats}.json seed file, keyed by its base name.
//
//go:embed *.json
var FS embed.FS

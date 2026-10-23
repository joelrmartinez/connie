// Package ats defines the normalized job model and the adapter contracts
// every ATS integration implements. Adapters live in subpackages (ashby,
// greenhouse, lever, workable); each is a thin fetch-and-map layer over one
// public, unauthenticated board API.
package ats

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrBoardNotFound reports that a board slug does not exist on the ATS.
// Adapters wrap it with the board name: fmt.Errorf("%s: %w", board, ErrBoardNotFound).
var ErrBoardNotFound = errors.New("board not found")

// Job is the normalized posting shared by all adapters.
type Job struct {
	ATS            string // "ashby", "greenhouse", "lever", "workable"
	Board          string // company slug on that ATS
	ExternalID     string // ATS-native posting id
	Title          string
	Location       string
	Department     string
	Team           string
	EmploymentType string
	Remote         bool
	URL            string
	PublishedAt    time.Time // zero if the ATS doesn't provide it
	Description    string    // plain-ish text; empty unless FetchOptions.Descriptions
	Pay            Pay       // structured pay when the ATS states it; zero when unknown
}

// Key is the cross-run identity: "ats:board:external_id". Namespacing by
// ATS and board is what lets one Registry hold all sources.
func (j Job) Key() string {
	return fmt.Sprintf("%s:%s:%s", j.ATS, j.Board, j.ExternalID)
}

// FetchOptions tunes a board fetch.
type FetchOptions struct {
	// Descriptions retains posting description text. Off by default:
	// descriptions are ~95% of payload and only --grep needs them.
	Descriptions bool
}

// Adapter fetches all live postings for one board. Implementations must
// honor ctx and return ErrBoardNotFound (wrapped) when the board is gone.
type Adapter interface {
	Name() string
	Fetch(ctx context.Context, board string, opt FetchOptions) ([]Job, error)
}

// Getter is the slice of the HTTP client adapters consume (defined here, at
// the point of use — httpx.Client satisfies it, tests pass a stub).
type Getter interface {
	Get(ctx context.Context, url string) ([]byte, error)
	Head(ctx context.Context, url string) error
}

// Discoverer is implemented by adapters that support archive-based board
// discovery. Separate from Adapter so a future ATS can ship fetch-only with
// a curated seed file.
type Discoverer interface {
	Hosts() []string                                       // CDX query targets, e.g. {"jobs.lever.co"}
	SlugFromURL(rawURL string) (string, bool)              // extract a candidate board slug
	Plausible(slug string) bool                            // cheap shape filter before probing
	Probe(ctx context.Context, board string) (bool, error) // cheapest liveness check
}

// CDXTarget is one archive query: a URL and the CDX match mode to use.
type CDXTarget struct {
	URL       string // e.g. "boards.greenhouse.io/embed/job_board"
	MatchType string // "domain", "host", "prefix", or "exact"
}

// CDXTargeter is an optional refinement of Discoverer for ATSes whose
// archive footprint is too large to query a whole domain at once, or whose
// slugs hide on one specific path. Greenhouse is both: a domain-wide query
// times out, while the embed path alone yields thousands of slugs in
// seconds. Adapters that do not implement it fall back to Hosts() with a
// domain match, which is the right default for a small footprint.
type CDXTargeter interface {
	CDXTargets() []CDXTarget
}

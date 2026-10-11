// Package workable fetches postings from Workable's public widget API.
// Endpoint: https://apply.workable.com/api/v1/widget/accounts/{slug} — no auth.
// ?details=true adds the full description and roughly doubles the payload, so
// it is requested only when descriptions are needed.
//
// Etiquette, verified 2026-08: apply.workable.com/robots.txt is an empty
// Disallow (allow all) and carries "Content-Signal: search=yes, ai-input=yes,
// ai-train=no". Reading these postings as input to an extraction step is
// expressly permitted; training a model on them is not.
//
// An unknown account returns 404, so a dead board is distinguishable from an
// empty one. That is not true of every ATS and it is what makes the Registry's
// close pass safe here.
package workable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/joelrmartinez/connie/ats"
	"github.com/joelrmartinez/connie/internal/httpx"
)

const endpoint = "https://apply.workable.com/api/v1/widget/accounts/"

// Adapter implements ats.Adapter for Workable.
type Adapter struct {
	client ats.Getter
}

// New returns a Workable adapter using client for HTTP.
func New(client ats.Getter) *Adapter { return &Adapter{client: client} }

// Name implements ats.Adapter.
func (a *Adapter) Name() string { return "workable" }

type response struct {
	Name string `json:"name"`
	Jobs []struct {
		Shortcode      string `json:"shortcode"`
		Title          string `json:"title"`
		URL            string `json:"url"`
		Department     string `json:"department"`
		Function       string `json:"function"`
		EmploymentType string `json:"employment_type"`
		Telecommuting  bool   `json:"telecommuting"`
		City           string `json:"city"`
		State          string `json:"state"`
		Country        string `json:"country"`
		PublishedOn    string `json:"published_on"` // bare date, not RFC3339
		Description    string `json:"description"`  // details=true only
	} `json:"jobs"`
}

// Fetch implements ats.Adapter.
func (a *Adapter) Fetch(ctx context.Context, slug string, opt ats.FetchOptions) ([]ats.Job, error) {
	u := endpoint + url.PathEscape(slug)
	if opt.Descriptions {
		u += "?details=true"
	}

	body, err := a.client.Get(ctx, u)
	if err != nil {
		if errors.Is(err, httpx.ErrNotFound) {
			return nil, fmt.Errorf("workable board %q: %w", slug, ats.ErrBoardNotFound)
		}
		return nil, fmt.Errorf("fetch workable board %q: %w", slug, err)
	}

	var r response
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("parse workable board %q: %w", slug, err)
	}
	if r.Jobs == nil {
		return nil, fmt.Errorf("parse workable board %q: no jobs array (API shape changed?)", slug)
	}

	jobs := make([]ats.Job, 0, len(r.Jobs))
	for _, j := range r.Jobs {
		job := ats.Job{
			ATS:            "workable",
			Board:          slug,
			ExternalID:     j.Shortcode,
			Title:          j.Title,
			Location:       joinLocation(j.City, j.State, j.Country),
			Department:     j.Department,
			Team:           j.Function,
			EmploymentType: j.EmploymentType,
			// Stated as a boolean rather than inferred from a location string.
			Remote: j.Telecommuting,
			URL:    j.URL,
		}
		if t, err := time.Parse("2006-01-02", j.PublishedOn); err == nil {
			job.PublishedAt = t.UTC()
		}
		if opt.Descriptions {
			job.Description = j.Description
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

// joinLocation composes the parts Workable supplies separately, skipping the
// empty ones so a remote-only posting does not read ", , United States".
func joinLocation(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, ", ")
}

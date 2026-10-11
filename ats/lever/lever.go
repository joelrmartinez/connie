// Package lever fetches postings from Lever's public postings API.
// Endpoint: https://api.lever.co/v0/postings/{slug}?mode=json — no auth.
// The response is a bare JSON array; createdAt is epoch milliseconds.
package lever

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

const endpoint = "https://api.lever.co/v0/postings/"

// Adapter implements ats.Adapter for Lever.
type Adapter struct {
	client ats.Getter
}

// New returns a Lever adapter using client for HTTP.
func New(client ats.Getter) *Adapter {
	return &Adapter{client: client}
}

// Name implements ats.Adapter.
func (a *Adapter) Name() string { return "lever" }

type posting struct {
	ID            string `json:"id"`
	Text          string `json:"text"` // the title
	CreatedAt     int64  `json:"createdAt"`
	HostedURL     string `json:"hostedUrl"`
	WorkplaceType string `json:"workplaceType"` // "remote" | "hybrid" | "onsite" | ""
	Categories    struct {
		Location   string `json:"location"`
		Team       string `json:"team"`
		Commitment string `json:"commitment"`
	} `json:"categories"`
	DescriptionPlain string `json:"descriptionPlain"`
	Lists            []struct {
		Text    string `json:"text"`
		Content string `json:"content"`
	} `json:"lists"`
}

// Fetch implements ats.Adapter.
func (a *Adapter) Fetch(ctx context.Context, slug string, opt ats.FetchOptions) ([]ats.Job, error) {
	body, err := a.client.Get(ctx, endpoint+url.PathEscape(slug)+"?mode=json")
	if err != nil {
		if errors.Is(err, httpx.ErrNotFound) {
			return nil, fmt.Errorf("lever board %q: %w", slug, ats.ErrBoardNotFound)
		}
		return nil, fmt.Errorf("fetch lever board %q: %w", slug, err)
	}

	// A bare array is the contract; an object (e.g. {"ok":false,...}) means
	// either an error payload or an API shape change — fail loud.
	var postings []posting
	if err := json.Unmarshal(body, &postings); err != nil {
		return nil, fmt.Errorf("parse lever board %q: %w", slug, err)
	}

	jobs := make([]ats.Job, 0, len(postings))
	for _, p := range postings {
		job := ats.Job{
			ATS:            "lever",
			Board:          slug,
			ExternalID:     p.ID,
			Title:          p.Text,
			Location:       p.Categories.Location,
			Team:           p.Categories.Team,
			EmploymentType: p.Categories.Commitment,
			Remote: strings.EqualFold(p.WorkplaceType, "remote") ||
				strings.Contains(strings.ToLower(p.Categories.Location), "remote"),
			URL: p.HostedURL,
		}
		if p.CreatedAt > 0 {
			job.PublishedAt = time.UnixMilli(p.CreatedAt).UTC()
		}
		if opt.Descriptions {
			// descriptionPlain plus every list section (requirements, benefits,
			// ...) — the lists are where the real keywords live (lever.py lesson).
			parts := []string{p.DescriptionPlain}
			for _, l := range p.Lists {
				parts = append(parts, l.Text, l.Content)
			}
			job.Description = strings.Join(parts, "\n")
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

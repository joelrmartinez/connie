// Package smartrecruiters fetches postings from SmartRecruiters' public
// Posting API. List: https://api.smartrecruiters.com/v1/companies/{slug}/postings
// (paginated, limit<=100, no auth). The list carries metadata only; descriptions
// require a per-posting detail fetch (.../postings/{id}), whose jobAd.sections
// hold the HTML body. Off by default, so --title scans stay to one request/page.
package smartrecruiters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/saiwrd/connie/internal/ats"
	"github.com/saiwrd/connie/internal/httpx"
)

const (
	apiBase    = "https://api.smartrecruiters.com/v1/companies/"
	publicBase = "https://jobs.smartrecruiters.com/"
	pageSize   = 100
	maxPages   = 100 // safety cap against a paging bug looping forever
)

// Adapter implements ats.Adapter for SmartRecruiters.
type Adapter struct {
	client ats.Getter
}

// New returns a SmartRecruiters adapter using client for HTTP.
func New(client ats.Getter) *Adapter { return &Adapter{client: client} }

// Name implements ats.Adapter.
func (a *Adapter) Name() string { return "smartrecruiters" }

type listResponse struct {
	TotalFound int       `json:"totalFound"`
	Content    []posting `json:"content"`
}

type posting struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ReleasedDate string `json:"releasedDate"`
	Location     struct {
		FullLocation string `json:"fullLocation"`
		City         string `json:"city"`
		Region       string `json:"region"`
		Country      string `json:"country"`
		Remote       bool   `json:"remote"`
	} `json:"location"`
	Department       label `json:"department"`
	Function         label `json:"function"`
	TypeOfEmployment label `json:"typeOfEmployment"`
}

type label struct {
	Label string `json:"label"`
}

type detailResponse struct {
	JobAd struct {
		Sections map[string]struct {
			Text string `json:"text"`
		} `json:"sections"`
	} `json:"jobAd"`
}

// Fetch implements ats.Adapter. It pages the list endpoint until every
// posting is collected, mapping each into the normalized ats.Job. When
// opt.Descriptions is set it also fetches each posting's detail for the body.
func (a *Adapter) Fetch(ctx context.Context, slug string, opt ats.FetchOptions) ([]ats.Job, error) {
	var jobs []ats.Job
	offset := 0
	for page := 0; page < maxPages; page++ {
		u := fmt.Sprintf("%s%s/postings?limit=%d&offset=%d", apiBase, url.PathEscape(slug), pageSize, offset)
		body, err := a.client.Get(ctx, u)
		if err != nil {
			if errors.Is(err, httpx.ErrNotFound) {
				return nil, fmt.Errorf("smartrecruiters board %q: %w", slug, ats.ErrBoardNotFound)
			}
			return nil, fmt.Errorf("fetch smartrecruiters board %q: %w", slug, err)
		}

		var r listResponse
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, fmt.Errorf("parse smartrecruiters board %q: %w", slug, err)
		}
		if r.Content == nil {
			return nil, fmt.Errorf("parse smartrecruiters board %q: no content array (API shape changed?)", slug)
		}

		for _, p := range r.Content {
			job := ats.Job{
				ATS:            "smartrecruiters",
				Board:          slug,
				ExternalID:     p.ID,
				Title:          p.Name,
				Location:       locationOf(p),
				Department:     p.Department.Label,
				Team:           p.Function.Label,
				EmploymentType: p.TypeOfEmployment.Label,
				Remote:         p.Location.Remote,
				URL:            publicBase + url.PathEscape(slug) + "/" + url.PathEscape(p.ID),
			}
			if t, err := time.Parse(time.RFC3339, p.ReleasedDate); err == nil {
				job.PublishedAt = t
			}
			if opt.Descriptions {
				if d, err := a.description(ctx, slug, p.ID); err == nil {
					job.Description = d
				}
			}
			jobs = append(jobs, job)
		}

		offset += len(r.Content)
		if len(r.Content) == 0 || offset >= r.TotalFound {
			break
		}
	}
	return jobs, nil
}

// description fetches one posting's detail and joins its jobAd sections in a
// stable order. Sections are single-escaped HTML; tag-stripping is
// filter.PlainText's job, so we leave the markup intact here.
func (a *Adapter) description(ctx context.Context, slug, id string) (string, error) {
	u := fmt.Sprintf("%s%s/postings/%s", apiBase, url.PathEscape(slug), url.PathEscape(id))
	body, err := a.client.Get(ctx, u)
	if err != nil {
		return "", err
	}
	var d detailResponse
	if err := json.Unmarshal(body, &d); err != nil {
		return "", err
	}
	var parts []string
	for _, k := range []string{"companyDescription", "jobDescription", "qualifications", "additionalInformation"} {
		if s, ok := d.JobAd.Sections[k]; ok && strings.TrimSpace(s.Text) != "" {
			parts = append(parts, s.Text)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

// locationOf prefers the server's fullLocation string and falls back to
// composing city/region/country when it is absent.
func locationOf(p posting) string {
	if s := strings.TrimSpace(p.Location.FullLocation); s != "" {
		return s
	}
	var parts []string
	for _, v := range []string{p.Location.City, p.Location.Region, p.Location.Country} {
		if v = strings.TrimSpace(v); v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, ", ")
}

// Package ashby fetches postings from Ashby's public posting API.
// Endpoint: https://api.ashbyhq.com/posting-api/job-board/{slug} — no auth.
// ?includeCompensation=true adds each posting's pay summary.
package ashby

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/joelrmartinez/connie/ats"
	"github.com/joelrmartinez/connie/internal/httpx"
)

const endpoint = "https://api.ashbyhq.com/posting-api/job-board/"

// Adapter implements ats.Adapter for Ashby.
type Adapter struct {
	client ats.Getter
}

// New returns an Ashby adapter using client for HTTP.
func New(client ats.Getter) *Adapter {
	return &Adapter{client: client}
}

// Name implements ats.Adapter.
func (a *Adapter) Name() string { return "ashby" }

// board mirrors the posting-api response. A missing jobs key means the API
// changed shape — fail loud rather than report an empty board.
type board struct {
	APIVersion string `json:"apiVersion"`
	Jobs       []struct {
		ID               string `json:"id"`
		Title            string `json:"title"`
		Department       string `json:"department"`
		Team             string `json:"team"`
		EmploymentType   string `json:"employmentType"`
		Location         string `json:"location"`
		IsRemote         bool   `json:"isRemote"`
		IsListed         bool   `json:"isListed"`
		PublishedAt      string `json:"publishedAt"`
		JobURL           string `json:"jobUrl"`
		DescriptionPlain string `json:"descriptionPlain"`
		Compensation     *struct {
			SummaryComponents []component `json:"summaryComponents"`
		} `json:"compensation"` // includeCompensation=true only
	} `json:"jobs"`
}

// component is one piece of an Ashby compensation summary: salary, equity,
// bonus, and so on. Only "Salary" becomes Pay.
type component struct {
	CompensationType string   `json:"compensationType"`
	Interval         string   `json:"interval"` // e.g. "1 YEAR", "1 HOUR"
	CurrencyCode     string   `json:"currencyCode"`
	MinValue         *float64 `json:"minValue"`
	MaxValue         *float64 `json:"maxValue"`
}

// salary picks the salary component, if any, as Pay.
func salary(cs []component) ats.Pay {
	for _, c := range cs {
		if c.CompensationType != "Salary" || (c.MinValue == nil && c.MaxValue == nil) {
			continue
		}
		p := ats.Pay{Currency: c.CurrencyCode, Period: ats.PeriodFromText(c.Interval), Source: ats.PayFromField}
		if c.MinValue != nil {
			p.Min = *c.MinValue
		}
		if c.MaxValue != nil {
			p.Max = *c.MaxValue
		}
		return p
	}
	return ats.Pay{}
}

// Fetch implements ats.Adapter. Unlisted postings (isListed=false) are
// internal-only and always dropped. Ashby slugs may contain spaces, so the
// slug is path-escaped.
func (a *Adapter) Fetch(ctx context.Context, slug string, opt ats.FetchOptions) ([]ats.Job, error) {
	body, err := a.client.Get(ctx, endpoint+url.PathEscape(slug)+"?includeCompensation=true")
	if err != nil {
		if errors.Is(err, httpx.ErrNotFound) {
			return nil, fmt.Errorf("ashby board %q: %w", slug, ats.ErrBoardNotFound)
		}
		return nil, fmt.Errorf("fetch ashby board %q: %w", slug, err)
	}

	var b board
	if err := json.Unmarshal(body, &b); err != nil {
		return nil, fmt.Errorf("parse ashby board %q: %w", slug, err)
	}
	if b.Jobs == nil {
		return nil, fmt.Errorf("parse ashby board %q: no jobs array (API shape changed?)", slug)
	}

	jobs := make([]ats.Job, 0, len(b.Jobs))
	for _, j := range b.Jobs {
		if !j.IsListed {
			continue
		}
		job := ats.Job{
			ATS:            "ashby",
			Board:          slug,
			ExternalID:     j.ID,
			Title:          j.Title,
			Location:       j.Location,
			Department:     j.Department,
			Team:           j.Team,
			EmploymentType: j.EmploymentType,
			Remote:         j.IsRemote,
			URL:            j.JobURL,
		}
		if t, err := time.Parse(time.RFC3339, j.PublishedAt); err == nil {
			job.PublishedAt = t
		}
		if j.Compensation != nil {
			job.Pay = salary(j.Compensation.SummaryComponents)
		}
		if opt.Descriptions {
			job.Description = j.DescriptionPlain
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

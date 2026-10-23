// Package greenhouse fetches postings from Greenhouse's public boards API.
// Endpoint: https://boards-api.greenhouse.io/v1/boards/{slug}/jobs — no auth.
// ?content=true roughly doubles the payload and is requested only when
// descriptions are needed; it is also the only variant that ships
// departments and offices. ?pay_transparency=true adds pay ranges.
package greenhouse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/joelrmartinez/connie/ats"
	"github.com/joelrmartinez/connie/internal/httpx"
)

const endpoint = "https://boards-api.greenhouse.io/v1/boards/"

// Adapter implements ats.Adapter for Greenhouse.
type Adapter struct {
	client ats.Getter
}

// New returns a Greenhouse adapter using client for HTTP.
func New(client ats.Getter) *Adapter {
	return &Adapter{client: client}
}

// Name implements ats.Adapter.
func (a *Adapter) Name() string { return "greenhouse" }

type response struct {
	Jobs []struct {
		ID             int64  `json:"id"`
		Title          string `json:"title"`
		FirstPublished string `json:"first_published"`
		AbsoluteURL    string `json:"absolute_url"`
		Location       struct {
			Name string `json:"name"`
		} `json:"location"`
		Content     string `json:"content"` // HTML-escaped HTML; content=true only
		Departments []struct {
			Name string `json:"name"`
		} `json:"departments"` // content=true only
		PayInputRanges []payRange `json:"pay_input_ranges"` // pay_transparency=true only
	} `json:"jobs"`
}

// payRange is one Greenhouse pay transparency range, in cents. Boards
// often post several (one per zone or tier).
type payRange struct {
	MinCents     int64  `json:"min_cents"`
	MaxCents     int64  `json:"max_cents"`
	CurrencyType string `json:"currency_type"`
}

// hourlyCeiling separates hourly from annual ranges. Greenhouse does not
// state a period, and no annual salary is under 1,000.
const hourlyCeiling = 1000

// payOf folds every range in the first range's currency into one span:
// the lowest minimum to the highest maximum.
func payOf(rs []payRange) ats.Pay {
	var p ats.Pay
	for _, r := range rs {
		if r.MinCents <= 0 && r.MaxCents <= 0 {
			continue
		}
		if p.Source == "" {
			p = ats.Pay{Currency: r.CurrencyType, Source: ats.PayFromField}
		}
		if r.CurrencyType != p.Currency {
			continue
		}
		lo, hi := float64(r.MinCents)/100, float64(r.MaxCents)/100
		if lo > 0 && (p.Min == 0 || lo < p.Min) {
			p.Min = lo
		}
		if hi > p.Max {
			p.Max = hi
		}
	}
	if p.Source != "" {
		p.Period = ats.PerYear
		if max(p.Min, p.Max) < hourlyCeiling {
			p.Period = ats.PerHour
		}
	}
	return p
}

// Fetch implements ats.Adapter.
func (a *Adapter) Fetch(ctx context.Context, slug string, opt ats.FetchOptions) ([]ats.Job, error) {
	u := endpoint + url.PathEscape(slug) + "/jobs?pay_transparency=true"
	if opt.Descriptions {
		u += "&content=true"
	}

	body, err := a.client.Get(ctx, u)
	if err != nil {
		if errors.Is(err, httpx.ErrNotFound) {
			return nil, fmt.Errorf("greenhouse board %q: %w", slug, ats.ErrBoardNotFound)
		}
		return nil, fmt.Errorf("fetch greenhouse board %q: %w", slug, err)
	}

	var r response
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("parse greenhouse board %q: %w", slug, err)
	}
	if r.Jobs == nil {
		return nil, fmt.Errorf("parse greenhouse board %q: no jobs array (API shape changed?)", slug)
	}

	jobs := make([]ats.Job, 0, len(r.Jobs))
	for _, j := range r.Jobs {
		var depts []string
		for _, d := range j.Departments {
			depts = append(depts, d.Name)
		}
		job := ats.Job{
			ATS:        "greenhouse",
			Board:      slug,
			ExternalID: strconv.FormatInt(j.ID, 10),
			Title:      j.Title,
			Location:   j.Location.Name,
			Department: strings.Join(depts, ", "),
			Remote:     strings.Contains(strings.ToLower(j.Location.Name), "remote"),
			URL:        j.AbsoluteURL,
			Pay:        payOf(j.PayInputRanges),
		}
		if t, err := time.Parse(time.RFC3339, j.FirstPublished); err == nil {
			job.PublishedAt = t
		}
		if opt.Descriptions {
			// Greenhouse double-escapes: the JSON string holds &lt;p&gt;-style
			// escaped HTML. Unescape once; tag-stripping is filter.PlainText's job.
			job.Description = html.UnescapeString(j.Content)
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

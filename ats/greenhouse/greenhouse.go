// Package greenhouse fetches postings from Greenhouse's public boards API.
// Endpoint: https://boards-api.greenhouse.io/v1/boards/{slug}/jobs — no auth.
// ?content=true roughly doubles the payload and is requested only when
// descriptions are needed; it is also the only variant that ships
// departments and offices.
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

	"github.com/saiwrd/connie/ats"
	"github.com/saiwrd/connie/internal/httpx"
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
	} `json:"jobs"`
}

// Fetch implements ats.Adapter.
func (a *Adapter) Fetch(ctx context.Context, slug string, opt ats.FetchOptions) ([]ats.Job, error) {
	u := endpoint + url.PathEscape(slug) + "/jobs"
	if opt.Descriptions {
		u += "?content=true"
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

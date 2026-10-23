package greenhouse

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/joelrmartinez/connie/ats"
	"github.com/joelrmartinez/connie/internal/httpx"
)

type stubGetter struct {
	body   []byte
	err    error
	gotURL string
}

func (s *stubGetter) Get(_ context.Context, url string) ([]byte, error) {
	s.gotURL = url
	return s.body, s.err
}

func (s *stubGetter) Head(_ context.Context, url string) error {
	s.gotURL = url
	return s.err
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

func TestFetch(t *testing.T) {
	t.Parallel()
	g := &stubGetter{body: fixture(t, "jobs.json")}
	jobs, err := New(g).Fetch(context.Background(), "cloudflare", ats.FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("Fetch() returned %d jobs, want 2", len(jobs))
	}
	// Plain endpoint must NOT ask for content.
	if strings.Contains(g.gotURL, "content=true") {
		t.Fatalf("plain fetch requested content: %q", g.gotURL)
	}
	for _, j := range jobs {
		if j.ATS != "greenhouse" || j.Board != "cloudflare" {
			t.Fatalf("job identity = %s/%s, want greenhouse/cloudflare", j.ATS, j.Board)
		}
		if j.ExternalID == "" || j.Title == "" || j.URL == "" || j.Location == "" {
			t.Fatalf("job missing core fields: %+v", j)
		}
		if j.PublishedAt.IsZero() {
			t.Fatalf("PublishedAt not parsed (first_published) for %q", j.Title)
		}
		if j.Description != "" {
			t.Fatalf("Description retained without opt: %q", j.Description)
		}
	}
}

func TestFetchDescriptions(t *testing.T) {
	t.Parallel()
	g := &stubGetter{body: fixture(t, "jobs_content.json")}
	jobs, err := New(g).Fetch(context.Background(), "cloudflare", ats.FetchOptions{Descriptions: true})
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	// Descriptions request must add content=true.
	if !strings.Contains(g.gotURL, "content=true") {
		t.Fatalf("descriptions fetch missing content=true: %q", g.gotURL)
	}
	for _, j := range jobs {
		// Greenhouse HTML-escapes content (&lt;p&gt;); adapter must unescape.
		if strings.Contains(j.Description, "&lt;") {
			t.Fatalf("Description still HTML-escaped: %q", j.Description)
		}
		if !strings.Contains(j.Description, "Kubernetes") {
			t.Fatalf("Description missing fixture text: %q", j.Description)
		}
		// departments only ship with content=true; the fixture has them.
		if j.Department == "" {
			t.Fatalf("Department not mapped from departments[]: %+v", j)
		}
	}
}

func TestFetchBoardNotFound(t *testing.T) {
	t.Parallel()
	g := &stubGetter{err: httpx.ErrNotFound}
	_, err := New(g).Fetch(context.Background(), "gone", ats.FetchOptions{})
	if !errors.Is(err, ats.ErrBoardNotFound) {
		t.Fatalf("Fetch() error = %v, want ats.ErrBoardNotFound", err)
	}
}

func TestFetchFailsLoudOnShapeChange(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`{"jobs": [`, `{"meta": {}}`} {
		g := &stubGetter{body: []byte(body)}
		if _, err := New(g).Fetch(context.Background(), "cloudflare", ats.FetchOptions{}); err == nil {
			t.Fatalf("Fetch(%q) error = nil, want loud failure", body)
		}
	}
}

func TestName(t *testing.T) {
	t.Parallel()
	if got := New(&stubGetter{}).Name(); got != "greenhouse" {
		t.Fatalf("Name() = %q, want greenhouse", got)
	}
}

func TestFetchPay(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("testdata/jobs_pay.json")
	if err != nil {
		t.Fatal(err)
	}
	g := &stubGetter{body: b}
	jobs, err := New(g).Fetch(context.Background(), "acme", ats.FetchOptions{Descriptions: true})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.Contains(g.gotURL, "pay_transparency=true") || !strings.Contains(g.gotURL, "content=true") {
		t.Fatalf("URL = %q", g.gotURL)
	}
	want := []ats.Pay{
		// Zones fold into one span; the CAD range is a different currency.
		{Min: 135000, Max: 190000, Currency: "USD", Period: ats.PerYear, Source: ats.PayFromField},
		{Min: 25, Max: 32, Currency: "USD", Period: ats.PerHour, Source: ats.PayFromField},
		{},
	}
	for i, w := range want {
		if jobs[i].Pay != w {
			t.Errorf("job %d pay = %+v, want %+v", i, jobs[i].Pay, w)
		}
	}
}

package lever

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/saiwrd/connie/internal/ats"
	"github.com/saiwrd/connie/internal/httpx"
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

func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/postings.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

func TestFetch(t *testing.T) {
	t.Parallel()
	g := &stubGetter{body: fixture(t)}
	jobs, err := New(g).Fetch(context.Background(), "palantir", ats.FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("Fetch() returned %d jobs, want 3", len(jobs))
	}
	if !strings.Contains(g.gotURL, "mode=json") {
		t.Fatalf("URL missing mode=json: %q", g.gotURL)
	}

	for _, j := range jobs {
		if j.ATS != "lever" || j.Board != "palantir" {
			t.Fatalf("job identity = %s/%s, want lever/palantir", j.ATS, j.Board)
		}
		if j.ExternalID == "" || j.Title == "" || j.URL == "" {
			t.Fatalf("job missing core fields: %+v", j)
		}
		// createdAt is epoch millis; the fixture's first posting is from 2024.
		if j.PublishedAt.Before(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("PublishedAt looks unparsed (epoch-millis bug): %v", j.PublishedAt)
		}
		if j.Description != "" {
			t.Fatalf("Description retained without opt: %q", j.Description)
		}
	}

	// Fixture: posting 0 is hybrid (must NOT count as remote), postings 1-2
	// are workplaceType=remote. Hybrid≠remote is the load-bearing assertion.
	if jobs[0].Remote {
		t.Fatalf("hybrid posting %q mapped as remote", jobs[0].Title)
	}
	var remotes int
	for _, j := range jobs {
		if j.Remote {
			remotes++
		}
	}
	if remotes != 2 {
		t.Fatalf("remote jobs = %d, want 2 (workplaceType mapping)", remotes)
	}

	// categories map into Team / Location / EmploymentType.
	first := jobs[0]
	if first.Location == "" || first.Team == "" || first.EmploymentType == "" {
		t.Fatalf("categories not mapped: %+v", first)
	}
}

func TestFetchDescriptions(t *testing.T) {
	t.Parallel()
	g := &stubGetter{body: fixture(t)}
	jobs, err := New(g).Fetch(context.Background(), "palantir", ats.FetchOptions{Descriptions: true})
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	for _, j := range jobs {
		if !strings.Contains(j.Description, "golang") {
			t.Fatalf("Description missing descriptionPlain text: %q", j.Description)
		}
		// lists[] content must be concatenated in (the lever.py lesson).
		if !strings.Contains(j.Description, "ship Go services") {
			t.Fatalf("Description missing lists[] content: %q", j.Description)
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
	// Lever returns a bare array; an object means the API changed shape.
	g := &stubGetter{body: []byte(`{"ok": false}`)}
	if _, err := New(g).Fetch(context.Background(), "palantir", ats.FetchOptions{}); err == nil {
		t.Fatal("Fetch() error = nil, want loud failure on non-array response")
	}
}

func TestName(t *testing.T) {
	t.Parallel()
	if got := New(&stubGetter{}).Name(); got != "lever" {
		t.Fatalf("Name() = %q, want lever", got)
	}
}

package workable

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/saiwrd/connie/ats"
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
	g := &stubGetter{body: fixture(t, "account.json")}
	jobs, err := New(g).Fetch(context.Background(), "blueground", ats.FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("Fetch() returned %d jobs, want 2", len(jobs))
	}
	// Descriptions double the payload, so a plain fetch must not ask for them.
	if strings.Contains(g.gotURL, "details=true") {
		t.Fatalf("plain fetch requested details: %q", g.gotURL)
	}
	for _, j := range jobs {
		if j.ATS != "workable" || j.Board != "blueground" {
			t.Fatalf("job identity = %s/%s, want workable/blueground", j.ATS, j.Board)
		}
		if j.ExternalID == "" || j.Title == "" || j.URL == "" {
			t.Fatalf("job missing core fields: %+v", j)
		}
		if j.Description != "" {
			t.Fatalf("Description retained without opt: %q", j.Description)
		}
	}
}

func TestFetchDescriptions(t *testing.T) {
	t.Parallel()
	g := &stubGetter{body: fixture(t, "account_details.json")}
	jobs, err := New(g).Fetch(context.Background(), "blueground", ats.FetchOptions{Descriptions: true})
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if !strings.Contains(g.gotURL, "details=true") {
		t.Fatalf("descriptions fetch missing details=true: %q", g.gotURL)
	}
	if jobs[0].Description == "" {
		t.Fatal("Description empty with opt.Descriptions")
	}
}

func TestLocationIsComposedFromCityStateCountry(t *testing.T) {
	t.Parallel()
	g := &stubGetter{body: []byte(`{"jobs":[
	  {"shortcode":"A1","title":"SDR","url":"https://apply.workable.com/j/A1",
	   "city":"Athens","state":"Attica","country":"Greece"}]}`)}
	jobs, _ := New(g).Fetch(context.Background(), "acme", ats.FetchOptions{})
	if jobs[0].Location != "Athens, Attica, Greece" {
		t.Fatalf("Location = %q, want the parts joined", jobs[0].Location)
	}
}

func TestLocationSkipsMissingParts(t *testing.T) {
	t.Parallel()
	g := &stubGetter{body: []byte(`{"jobs":[
	  {"shortcode":"A1","title":"SDR","url":"https://apply.workable.com/j/A1",
	   "city":"","state":"","country":"United States"}]}`)}
	jobs, _ := New(g).Fetch(context.Background(), "acme", ats.FetchOptions{})
	if jobs[0].Location != "United States" {
		t.Fatalf("Location = %q, want no stray commas", jobs[0].Location)
	}
}

func TestTelecommutingMapsToRemote(t *testing.T) {
	t.Parallel()
	// Workable states remoteness as a boolean rather than leaving it to be
	// guessed from a location string, so use it.
	g := &stubGetter{body: []byte(`{"jobs":[
	  {"shortcode":"A1","title":"SDR","url":"u","telecommuting":true},
	  {"shortcode":"A2","title":"AE","url":"u","telecommuting":false}]}`)}
	jobs, _ := New(g).Fetch(context.Background(), "acme", ats.FetchOptions{})
	if !jobs[0].Remote {
		t.Fatal("telecommuting:true did not map to Remote")
	}
	if jobs[1].Remote {
		t.Fatal("telecommuting:false mapped to Remote")
	}
}

func TestPublishedDateIsParsed(t *testing.T) {
	t.Parallel()
	// published_on is a bare date, not RFC3339, so the shared parser misses it.
	g := &stubGetter{body: []byte(`{"jobs":[
	  {"shortcode":"A1","title":"SDR","url":"u","published_on":"2026-02-12"}]}`)}
	jobs, _ := New(g).Fetch(context.Background(), "acme", ats.FetchOptions{})
	if jobs[0].PublishedAt.IsZero() {
		t.Fatal("published_on not parsed")
	}
	if got := jobs[0].PublishedAt.Format("2006-01-02"); got != "2026-02-12" {
		t.Fatalf("PublishedAt = %s, want 2026-02-12", got)
	}
}

func TestFetchBoardNotFound(t *testing.T) {
	t.Parallel()
	// Verified live: an unknown account returns 404, so a dead board is
	// distinguishable from an empty one. Unlike some peers, this API does not
	// answer 200 with an empty list.
	g := &stubGetter{err: httpx.ErrNotFound}
	_, err := New(g).Fetch(context.Background(), "gone", ats.FetchOptions{})
	if !errors.Is(err, ats.ErrBoardNotFound) {
		t.Fatalf("Fetch() error = %v, want ats.ErrBoardNotFound", err)
	}
}

func TestFetchFailsLoudOnShapeChange(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`{"jobs": [`, `{"name": "acme"}`} {
		g := &stubGetter{body: []byte(body)}
		if _, err := New(g).Fetch(context.Background(), "acme", ats.FetchOptions{}); err == nil {
			t.Fatalf("Fetch(%q) error = nil, want loud failure", body)
		}
	}
}

func TestName(t *testing.T) {
	t.Parallel()
	if got := New(&stubGetter{}).Name(); got != "workable" {
		t.Fatalf("Name() = %q, want workable", got)
	}
}

func TestSlugFromURL(t *testing.T) {
	t.Parallel()
	a := New(&stubGetter{})
	tests := []struct {
		url  string
		want string
		ok   bool
	}{
		{"https://apply.workable.com/blueground/", "blueground", true},
		{"https://apply.workable.com/blueground/j/186545F8C1/", "blueground", true},
		{"https://blueground.workable.com/", "blueground", true},
		{"https://apply.workable.com/", "", false},
		{"https://apply.workable.com/j/186545F8C1", "", false}, // shortcode, not a board
		{"https://example.com/blueground", "", false},
	}
	for _, tt := range tests {
		got, ok := a.SlugFromURL(tt.url)
		if got != tt.want || ok != tt.ok {
			t.Errorf("SlugFromURL(%q) = (%q, %v), want (%q, %v)", tt.url, got, ok, tt.want, tt.ok)
		}
	}
}

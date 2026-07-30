package smartrecruiters

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/saiwrd/connie/internal/ats"
	"github.com/saiwrd/connie/internal/httpx"
)

// routeGetter serves list pages (keyed by the offset query param) and a single
// detail body, so a test can exercise pagination and the descriptions path.
type routeGetter struct {
	list   map[int][]byte
	detail []byte
	err    error
	calls  []string
}

func (g *routeGetter) Get(_ context.Context, u string) ([]byte, error) {
	g.calls = append(g.calls, u)
	if g.err != nil {
		return nil, g.err
	}
	if strings.Contains(u, "/postings?") {
		off := 0
		if i := strings.Index(u, "offset="); i >= 0 {
			fmt.Sscanf(u[i+len("offset="):], "%d", &off)
		}
		if b, ok := g.list[off]; ok {
			return b, nil
		}
		return g.list[0], nil
	}
	return g.detail, nil // a /postings/{id} detail request
}

func (g *routeGetter) Head(_ context.Context, u string) error {
	g.calls = append(g.calls, u)
	return g.err
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
	g := &routeGetter{list: map[int][]byte{0: fixture(t, "postings.json")}}
	jobs, err := New(g).Fetch(context.Background(), "OnTheStage", ats.FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("Fetch() returned %d jobs, want 2", len(jobs))
	}
	// Plain fetch must not touch a detail endpoint.
	for _, c := range g.calls {
		if strings.Contains(c, "/postings/") {
			t.Fatalf("plain fetch hit a detail endpoint: %q", c)
		}
	}
	var sawRemote bool
	for _, j := range jobs {
		if j.ATS != "smartrecruiters" || j.Board != "OnTheStage" {
			t.Fatalf("job identity = %s/%s, want smartrecruiters/OnTheStage", j.ATS, j.Board)
		}
		if j.ExternalID == "" || j.Title == "" || j.Location == "" {
			t.Fatalf("job missing core fields: %+v", j)
		}
		// Public apply host, not the api host, and id-addressed.
		if !strings.HasPrefix(j.URL, "https://jobs.smartrecruiters.com/OnTheStage/") {
			t.Fatalf("URL not the public board form: %q", j.URL)
		}
		if strings.Contains(j.URL, "api.smartrecruiters.com") {
			t.Fatalf("URL points at the API host: %q", j.URL)
		}
		if j.PublishedAt.IsZero() {
			t.Fatalf("PublishedAt not parsed (releasedDate) for %q", j.Title)
		}
		if j.Description != "" {
			t.Fatalf("Description retained without opt: %q", j.Description)
		}
		if j.Remote {
			sawRemote = true
		}
	}
	// The GTM Operations Manager posting in the fixture is remote:true.
	if !sawRemote {
		t.Fatalf("Remote never mapped true from location.remote")
	}
}

func TestFetchDescriptions(t *testing.T) {
	t.Parallel()
	g := &routeGetter{
		list:   map[int][]byte{0: fixture(t, "postings.json")},
		detail: fixture(t, "posting_detail.json"),
	}
	jobs, err := New(g).Fetch(context.Background(), "OnTheStage", ats.FetchOptions{Descriptions: true})
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	// A detail endpoint must have been hit.
	var sawDetail bool
	for _, c := range g.calls {
		if strings.Contains(c, "/postings/744000136114322") {
			sawDetail = true
		}
	}
	if !sawDetail {
		t.Fatalf("descriptions fetch never hit the posting detail endpoint; calls=%v", g.calls)
	}
	if jobs[0].Description == "" {
		t.Fatalf("Description not populated with opt.Descriptions")
	}
	// jobDescription section text from the fixture.
	if !strings.Contains(jobs[0].Description, "GTM Operations Manager") {
		t.Fatalf("Description missing fixture section text: %q", jobs[0].Description)
	}
}

func TestFetchPaginates(t *testing.T) {
	t.Parallel()
	// totalFound=3 across two pages: two on the first, one on the second.
	page0 := []byte(`{"totalFound":3,"content":[
		{"id":"1","name":"A","location":{"fullLocation":"Remote","remote":true}},
		{"id":"2","name":"B","location":{"fullLocation":"NYC"}}]}`)
	page1 := []byte(`{"totalFound":3,"content":[
		{"id":"3","name":"C","location":{"fullLocation":"SF"}}]}`)
	g := &routeGetter{list: map[int][]byte{0: page0, 2: page1}}
	jobs, err := New(g).Fetch(context.Background(), "acme", ats.FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("paginated Fetch() returned %d jobs, want 3", len(jobs))
	}
	if jobs[2].ExternalID != "3" {
		t.Fatalf("second page not fetched; last id = %q, want 3", jobs[2].ExternalID)
	}
}

func TestFetchBoardNotFound(t *testing.T) {
	t.Parallel()
	g := &routeGetter{err: httpx.ErrNotFound}
	_, err := New(g).Fetch(context.Background(), "gone", ats.FetchOptions{})
	if !errors.Is(err, ats.ErrBoardNotFound) {
		t.Fatalf("Fetch() error = %v, want ats.ErrBoardNotFound", err)
	}
}

func TestFetchFailsLoudOnShapeChange(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`{"content": [`, `{"meta": {}}`} {
		g := &routeGetter{list: map[int][]byte{0: []byte(body)}}
		if _, err := New(g).Fetch(context.Background(), "acme", ats.FetchOptions{}); err == nil {
			t.Fatalf("Fetch(%q) error = nil, want loud failure", body)
		}
	}
}

func TestName(t *testing.T) {
	t.Parallel()
	if got := New(&routeGetter{}).Name(); got != "smartrecruiters" {
		t.Fatalf("Name() = %q, want smartrecruiters", got)
	}
}

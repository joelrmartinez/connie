package ashby

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/saiwrd/connie/ats"
	"github.com/saiwrd/connie/internal/httpx"
)

// stubGetter feeds canned responses to the adapter and records the URL asked for.
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
	b, err := os.ReadFile("testdata/board.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

func TestFetch(t *testing.T) {
	t.Parallel()
	g := &stubGetter{body: fixture(t)}
	jobs, err := New(g).Fetch(context.Background(), "middesk", ats.FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	// Fixture holds 3 jobs; one is isListed=false and must be dropped.
	if len(jobs) != 2 {
		t.Fatalf("Fetch() returned %d jobs, want 2 (unlisted filtered)", len(jobs))
	}
	for _, j := range jobs {
		if strings.Contains(j.Title, "synthetic") {
			t.Fatalf("unlisted job leaked through: %q", j.Title)
		}
		if j.ATS != "ashby" || j.Board != "middesk" {
			t.Fatalf("job identity = %s/%s, want ashby/middesk", j.ATS, j.Board)
		}
		if j.ExternalID == "" || j.Title == "" || j.URL == "" {
			t.Fatalf("job missing core fields: %+v", j)
		}
		if j.PublishedAt.IsZero() {
			t.Fatalf("PublishedAt not parsed for %q", j.Title)
		}
		if j.Description != "" {
			t.Fatalf("Description retained without FetchOptions.Descriptions: %q", j.Description)
		}
		if j.Key() != "ashby:middesk:"+j.ExternalID {
			t.Fatalf("Key() = %q", j.Key())
		}
	}

	// Fixture has one remote and one onsite listed job.
	var remotes int
	for _, j := range jobs {
		if j.Remote {
			remotes++
		}
	}
	if remotes != 1 {
		t.Fatalf("remote jobs = %d, want 1", remotes)
	}
}

func TestFetchDescriptions(t *testing.T) {
	t.Parallel()
	g := &stubGetter{body: fixture(t)}
	jobs, err := New(g).Fetch(context.Background(), "middesk", ats.FetchOptions{Descriptions: true})
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	for _, j := range jobs {
		if !strings.Contains(j.Description, "kubernetes") {
			t.Fatalf("Description missing fixture text for %q: %q", j.Title, j.Description)
		}
	}
}

func TestFetchSlugEscaping(t *testing.T) {
	t.Parallel()
	// Ashby slugs may contain spaces ("Sample Board"); they must be path-escaped.
	g := &stubGetter{body: fixture(t)}
	if _, err := New(g).Fetch(context.Background(), "Sample Board", ats.FetchOptions{}); err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if strings.Contains(g.gotURL, " ") {
		t.Fatalf("URL contains raw space: %q", g.gotURL)
	}
	if !strings.Contains(g.gotURL, "Sample%20Board") {
		t.Fatalf("URL not path-escaped: %q", g.gotURL)
	}
}

func TestFetchBoardNotFound(t *testing.T) {
	t.Parallel()
	g := &stubGetter{err: httpx.ErrNotFound}
	_, err := New(g).Fetch(context.Background(), "no-such-board", ats.FetchOptions{})
	if !errors.Is(err, ats.ErrBoardNotFound) {
		t.Fatalf("Fetch() error = %v, want ats.ErrBoardNotFound", err)
	}
	if !strings.Contains(err.Error(), "no-such-board") {
		t.Fatalf("error does not name the board: %v", err)
	}
}

func TestFetchFailsLoudOnShapeChange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{"malformed json", `{"jobs": [`},
		{"missing jobs key", `{"apiVersion": "1"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := &stubGetter{body: []byte(tt.body)}
			if _, err := New(g).Fetch(context.Background(), "middesk", ats.FetchOptions{}); err == nil {
				t.Fatal("Fetch() error = nil, want loud failure on API shape change")
			}
		})
	}
}

func TestName(t *testing.T) {
	t.Parallel()
	if got := New(&stubGetter{}).Name(); got != "ashby" {
		t.Fatalf("Name() = %q, want ashby", got)
	}
}

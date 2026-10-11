package greenhouse

import (
	"context"
	"errors"
	"testing"

	"github.com/joelrmartinez/connie/internal/httpx"
)

func TestSlugFromURL(t *testing.T) {
	t.Parallel()
	a := New(&stubGetter{})
	tests := []struct {
		url  string
		want string
		ok   bool
	}{
		{"https://boards.greenhouse.io/cloudflare/jobs/123", "cloudflare", true},
		{"https://job-boards.greenhouse.io/stripe", "stripe", true}, // migrated host
		{"https://boards.greenhouse.io/", "", false},
		{"https://example.com/cloudflare", "", false},

		// The embed form. Companies iframe their board rather than link it,
		// so the slug lives in a query parameter and the path is a constant.
		// Reading only the first path segment yields "embed" for every one of
		// these, which is both wrong and, because it is a single plausible
		// token, silently wrong.
		{"https://boards.greenhouse.io/embed/job_board?for=airbnb", "airbnb", true},
		{"https://boards.greenhouse.io/embed/job_board?for=airbnb&b=https%3A%2F%2Fairbnb.com", "airbnb", true},
		{"https://boards.greenhouse.io/embed/job_app?for=doordash&token=123", "doordash", true},
		{"https://job-boards.greenhouse.io/embed/job_board?for=ramp", "ramp", true},
		{"https://boards.greenhouse.io/embed/job_board", "", false}, // no for=
		{"https://boards.greenhouse.io/embed/job_board?for=", "", false},
	}
	for _, tt := range tests {
		got, ok := a.SlugFromURL(tt.url)
		if got != tt.want || ok != tt.ok {
			t.Errorf("SlugFromURL(%q) = (%q, %v), want (%q, %v)", tt.url, got, ok, tt.want, tt.ok)
		}
	}
}

func TestPlausible(t *testing.T) {
	t.Parallel()
	a := New(&stubGetter{})
	if a.Plausible("A1 Garage Door") {
		t.Error("Greenhouse slugs must not contain spaces")
	}
	if !a.Plausible("cloudflare") {
		t.Error("real slug rejected")
	}
}

func TestProbe(t *testing.T) {
	t.Parallel()
	live := &stubGetter{}
	if ok, err := New(live).Probe(context.Background(), "cloudflare"); !ok || err != nil {
		t.Fatalf("Probe(live) = (%v, %v), want (true, nil)", ok, err)
	}
	if ok, err := New(&stubGetter{err: httpx.ErrNotFound}).Probe(context.Background(), "gone"); ok || err != nil {
		t.Fatalf("Probe(404) = (%v, %v), want (false, nil)", ok, err)
	}
	if _, err := New(&stubGetter{err: errors.New("boom")}).Probe(context.Background(), "x"); err == nil {
		t.Fatal("Probe(broken) error = nil, want surfaced")
	}
}

func TestHosts(t *testing.T) {
	t.Parallel()
	h := New(&stubGetter{}).Hosts()
	if len(h) != 2 {
		t.Fatalf("Hosts() = %v, want both greenhouse hosts", h)
	}
}

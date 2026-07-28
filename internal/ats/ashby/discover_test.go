package ashby

import (
	"context"
	"errors"
	"testing"

	"github.com/saiwrd/connie/internal/httpx"
)

func TestSlugFromURL(t *testing.T) {
	t.Parallel()
	a := New(&stubGetter{})
	tests := []struct {
		url  string
		want string
		ok   bool
	}{
		{"https://jobs.ashbyhq.com/ramp/abc-123?ref=x", "ramp", true},
		{"https://jobs.ashbyhq.com/A1%20Garage%20Door/x", "A1 Garage Door", true}, // percent-decoded
		{"https://jobs.ashbyhq.com/", "", false},                                  // no slug
		{"https://example.com/ramp", "", false},                                   // wrong host
		{"http://jobs.ashbyhq.com/posthog", "posthog", true},                      // scheme-insensitive
		{"://not a url", "", false},
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
	if !a.Plausible("A1 Garage Door") {
		t.Error("Ashby must allow spaces in slugs")
	}
	if a.Plausible("_next") {
		t.Error("archive noise passed plausibility")
	}
}

func TestProbe(t *testing.T) {
	t.Parallel()
	a := New(&stubGetter{})
	ok, err := a.Probe(context.Background(), "middesk")
	if err != nil || !ok {
		t.Fatalf("Probe(live) = (%v, %v), want (true, nil)", ok, err)
	}

	gone := New(&stubGetter{err: httpx.ErrNotFound})
	ok, err = gone.Probe(context.Background(), "nope")
	if err != nil || ok {
		t.Fatalf("Probe(404) = (%v, %v), want (false, nil) — a missing board is not an error", ok, err)
	}

	broken := New(&stubGetter{err: errors.New("boom")})
	if _, err := broken.Probe(context.Background(), "x"); err == nil {
		t.Fatal("Probe(broken) error = nil, want error surfaced")
	}
}

func TestHosts(t *testing.T) {
	t.Parallel()
	h := New(&stubGetter{}).Hosts()
	if len(h) != 1 || h[0] != "jobs.ashbyhq.com" {
		t.Fatalf("Hosts() = %v", h)
	}
}

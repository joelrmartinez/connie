package lever

import (
	"context"
	"errors"
	"strings"
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
		{"https://jobs.lever.co/palantir/uuid-here", "palantir", true},
		{"https://jobs.lever.co/", "", false},
		{"https://example.com/palantir", "", false},
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
	if a.Plausible("has space") {
		t.Error("Lever slugs must not contain spaces")
	}
	if !a.Plausible("palantir") {
		t.Error("real slug rejected")
	}
}

func TestProbe(t *testing.T) {
	t.Parallel()
	// Lever probes with a limit-1 GET, not HEAD.
	live := &stubGetter{body: []byte("[]")}
	if ok, err := New(live).Probe(context.Background(), "palantir"); !ok || err != nil {
		t.Fatalf("Probe(live) = (%v, %v), want (true, nil)", ok, err)
	}
	if !strings.Contains(live.gotURL, "limit=1") {
		t.Fatalf("probe URL not limit-1: %q", live.gotURL)
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
	if len(h) != 1 || h[0] != "jobs.lever.co" {
		t.Fatalf("Hosts() = %v", h)
	}
}

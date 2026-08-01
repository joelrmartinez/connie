package greenhouse

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
		{"https://boards.greenhouse.io/cloudflare/jobs/123", "cloudflare", true},
		{"https://job-boards.greenhouse.io/stripe", "stripe", true}, // migrated host
		{"https://boards.greenhouse.io/", "", false},
		{"https://example.com/cloudflare", "", false},
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

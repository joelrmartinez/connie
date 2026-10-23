package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingFileIsNil(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil || c != nil {
		t.Fatalf("Load(missing) = %v, %v; want nil, nil", c, err)
	}
}

func TestParseDefaults(t *testing.T) {
	c, err := Parse([]byte(""))
	if err != nil {
		t.Fatalf("Parse(empty): %v", err)
	}
	if c.Location.Remote != RemoteUS {
		t.Errorf("remote default = %q, want us", c.Location.Remote)
	}
	if c.Weights != DefaultWeights {
		t.Errorf("weights default = %+v", c.Weights)
	}
}

func TestParseRejects(t *testing.T) {
	tests := map[string]string{
		"unknown key":     "titel: [x]\n",
		"bad remote":      "location: {remote: mars}\n",
		"negative floor":  "pay: {floor: -1}\n",
		"negative weight": "weights: {title: -5}\n",
		"empty title":     "titles: ['  ']\n",
	}
	for name, doc := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(doc)); err == nil {
				t.Fatalf("Parse(%q) = nil error", doc)
			}
		})
	}
}

// The shipped starters must always load: they are the documentation.
func TestExamplesLoad(t *testing.T) {
	for _, name := range []string{"gtm.yaml", "sales.yaml"} {
		t.Run(name, func(t *testing.T) {
			c, err := Load(filepath.Join("..", "..", "examples", name))
			if err != nil || c == nil {
				t.Fatalf("Load: %v", err)
			}
			if len(c.Titles) == 0 || len(c.ATS) != 4 {
				t.Fatalf("titles=%v ats=%v", c.Titles, c.ATS)
			}
			if c.ATSList() != "ashby,greenhouse,lever,workable" {
				t.Fatalf("ATSList = %q", c.ATSList())
			}
		})
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	in := &Config{
		Titles:   []string{"GTM Engineer"},
		Variants: map[string][]string{"GTM Engineer": {"go-to-market engineer"}},
		Location: Location{Remote: RemoteUS, Cities: []string{"Seattle"}, Hybrid: true},
		Pay:      Pay{CountDescription: true},
		Weights:  DefaultWeights,
		ATS:      []string{"ashby"},
	}
	b, err := Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(out.Titles, ",") != "GTM Engineer" || !out.Location.Hybrid || !out.Pay.CountDescription {
		t.Fatalf("round trip lost data: %+v", out)
	}
}

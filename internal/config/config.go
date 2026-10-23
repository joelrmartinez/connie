// Package config loads connie.yaml, the saved search. A config file turns
// a search people retype every run into settings they edit once. Every
// field is optional; CLI flags always win over the file.
package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"
)

// DefaultPath is where scan and init look when --config is not given.
const DefaultPath = "connie.yaml"

// Remote scopes for Location.Remote.
const (
	RemoteUS       = "us"       // remote roles open to US residents
	RemoteAnywhere = "anywhere" // any remote role
	RemoteOff      = "off"      // remote roles do not count
)

// Config is one saved search.
type Config struct {
	// Titles are the job families wanted, e.g. "GTM Engineer".
	Titles []string `yaml:"titles"`
	// Variants maps a family to other names it goes by. Matching is
	// case-insensitive; the family name itself always counts.
	Variants map[string][]string `yaml:"variants,omitempty"`
	Location Location            `yaml:"location"`
	Pay      Pay                 `yaml:"pay"`
	Weights  Weights             `yaml:"weights"`
	// ATS lists the adapters to scan; empty means all.
	ATS []string `yaml:"ats,omitempty"`
}

// Location is where the user is willing to work.
type Location struct {
	Remote string   `yaml:"remote"`           // us, anywhere, or off
	Cities []string `yaml:"cities,omitempty"` // offices within reach
	Hybrid bool     `yaml:"hybrid"`           // hybrid or onsite roles in Cities count
}

// Pay is how compensation is treated.
type Pay struct {
	// Floor is an optional annual figure. Zero means no floor. It is a
	// score input, never a hard cutoff.
	Floor int `yaml:"floor,omitempty"`
	// CountDescription lets pay parsed from description text count.
	CountDescription bool `yaml:"count_description"`
}

// Weights are the ranking points per signal. They need not sum to 100.
type Weights struct {
	Title    int `yaml:"title"`
	Pay      int `yaml:"pay"`
	Location int `yaml:"location"`
}

// DefaultWeights are used when the file sets none.
var DefaultWeights = Weights{Title: 50, Pay: 30, Location: 20}

// Load reads and validates the file at path. A missing file is not an
// error: it returns (nil, nil) so callers fall back to flags alone.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	c, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("config %q: %w", path, err)
	}
	return c, nil
}

// Parse decodes YAML, fills defaults, and validates. Unknown keys are
// errors so a typo does not silently do nothing.
func Parse(b []byte) (*Config, error) {
	var c Config
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse: %w", err)
	}
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	c.Location.Remote = strings.ToLower(strings.TrimSpace(c.Location.Remote))
	if c.Location.Remote == "" {
		c.Location.Remote = RemoteUS
	}
	if c.Weights == (Weights{}) {
		c.Weights = DefaultWeights
	}
	for i, a := range c.ATS {
		c.ATS[i] = strings.ToLower(strings.TrimSpace(a))
	}
}

// Validate reports the first setting that cannot work.
func (c *Config) Validate() error {
	switch c.Location.Remote {
	case RemoteUS, RemoteAnywhere, RemoteOff:
	default:
		return fmt.Errorf("location.remote %q: want us, anywhere, or off", c.Location.Remote)
	}
	if c.Pay.Floor < 0 {
		return fmt.Errorf("pay.floor %d: must not be negative", c.Pay.Floor)
	}
	w := c.Weights
	if w.Title < 0 || w.Pay < 0 || w.Location < 0 {
		return fmt.Errorf("weights must not be negative")
	}
	for _, t := range c.Titles {
		if strings.TrimSpace(t) == "" {
			return fmt.Errorf("titles: empty entry")
		}
	}
	return nil
}

// ATSList returns the ATS names comma-joined, the shape --ats takes.
func (c *Config) ATSList() string { return strings.Join(c.ATS, ",") }

// Marshal renders c as YAML with two-space indents.
func Marshal(c *Config) ([]byte, error) {
	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(c); err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	return []byte(b.String()), nil
}

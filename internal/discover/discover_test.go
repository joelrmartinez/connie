package discover

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joelrmartinez/connie/ats"
	"github.com/joelrmartinez/connie/ats/ashby"
	"github.com/joelrmartinez/connie/ats/greenhouse"
	"github.com/joelrmartinez/connie/ats/lever"
)

// The real adapters must satisfy the Discoverer contract.
var (
	_ ats.Discoverer = (*ashby.Adapter)(nil)
	_ ats.Discoverer = (*greenhouse.Adapter)(nil)
	_ ats.Discoverer = (*lever.Adapter)(nil)
)

// cdxFixture mimics the Wayback CDX output=json shape: a header row then
// one-column URL rows, salted with the junk the real archive returns.
const cdxFixture = `[["original"],
["https://jobs.example.com/ramp/abc-123?ref=x"],
["https://jobs.example.com/Ramp/def-456"],
["https://jobs.example.com/A1%20Garage%20Door/x"],
["https://jobs.example.com/_next/static/z.js"],
["https://jobs.example.com/"],
["https://jobs.example.com/deadbeefdeadbeefdeadbeef/t"],
["https://jobs.example.com/posthog"],
["https://jobs.example.com/ghost-board/apply"]]`

// fakeDiscoverer discovers boards on jobs.example.com; "ghost-board" fails
// its probe (archived once, dead now).
type fakeDiscoverer struct {
	mu     sync.Mutex
	probed []string
}

func (f *fakeDiscoverer) Hosts() []string { return []string{"jobs.example.com"} }

func (f *fakeDiscoverer) SlugFromURL(raw string) (string, bool) {
	if !strings.Contains(raw, "jobs.example.com/") {
		return "", false
	}
	rest := strings.TrimPrefix(raw, "https://jobs.example.com/")
	seg := strings.SplitN(rest, "/", 2)[0]
	seg = strings.SplitN(seg, "?", 2)[0]
	seg = strings.ReplaceAll(seg, "%20", " ")
	if seg == "" {
		return "", false
	}
	return seg, true
}

func (f *fakeDiscoverer) Plausible(slug string) bool { return ats.PlausibleSlug(slug, true) }

func (f *fakeDiscoverer) Probe(_ context.Context, board string) (bool, error) {
	f.mu.Lock()
	f.probed = append(f.probed, board)
	f.mu.Unlock()
	return board != "ghost-board", nil
}

type stubGetter struct {
	body []byte
	err  error
}

func (s *stubGetter) Get(_ context.Context, url string) ([]byte, error) { return s.body, s.err }
func (s *stubGetter) Head(_ context.Context, url string) error          { return s.err }

func TestHarvest(t *testing.T) {
	t.Parallel()
	d := &fakeDiscoverer{}
	got, err := Harvest(context.Background(), d, &stubGetter{body: []byte(cdxFixture)}, Config{Concurrency: 2})
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}
	// ramp deduped case-insensitively (first casing wins), junk and dead
	// boards dropped, spaces decoded.
	want := []string{"A1 Garage Door", "posthog", "ramp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Harvest = %v, want %v", got, want)
	}
	// The dedupe must happen BEFORE probing: 4 candidates probed, not 5.
	if len(d.probed) != 4 {
		t.Fatalf("probed %d candidates, want 4 (dedupe before probe)", len(d.probed))
	}
}

func TestHarvestSurfacesCDXFailure(t *testing.T) {
	t.Parallel()
	_, err := Harvest(context.Background(), &fakeDiscoverer{}, &stubGetter{err: errors.New("cdx down")}, Config{})
	if err == nil {
		t.Fatal("Harvest() error = nil, want CDX failure surfaced")
	}
}

func TestUnionNeverShrinks(t *testing.T) {
	t.Parallel()
	seed := []string{"ramp", "posthog"}
	cache := []string{"Ramp", "middesk"} // dupe casing of ramp
	live := []string{"newco"}

	got := Union(seed, cache, live)
	want := []string{"middesk", "newco", "posthog", "ramp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Union = %v, want %v", got, want)
	}
	// Order of inputs must not lose anything: cache-only boards survive
	// even when the live crawl missed them.
	if !contains(got, "middesk") {
		t.Fatal("cache-only board lost")
	}
}

func TestLoadSaveRoundtrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "boards", "ashby.json")
	boards := []string{"ramp", "posthog", "A1 Garage Door"}

	if err := Save(path, boards); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, boards) {
		t.Fatalf("roundtrip = %v, want %v", got, boards)
	}
}

func TestLoadMissingFileIsEmptyNotError(t *testing.T) {
	t.Parallel()
	got, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("Load(missing) error = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("Load(missing) = %v, want nil", got)
	}
}

func TestLoadGarbageErrors(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := Save(path, []string{"x"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Overwrite with garbage via Save's own file... simpler: write direct.
	if err := writeFile(path, []byte("{not json")); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load(garbage) error = nil, want parse error")
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func fmtBoards(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("b%03d", i)
	}
	return out
}

func TestHarvestBoundsProbeConcurrency(t *testing.T) {
	t.Parallel()
	// A discoverer whose probe tracks in-flight count.
	d := &countingDiscoverer{max: make(chan int32, 1)}
	var rows []string
	for _, b := range fmtBoards(20) {
		rows = append(rows, fmt.Sprintf("[%q]", "https://jobs.example.com/"+b))
	}
	body := "[[\"original\"],\n" + strings.Join(rows, ",\n") + "]"

	if _, err := Harvest(context.Background(), d, &stubGetter{body: []byte(body)}, Config{Concurrency: 3}); err != nil {
		t.Fatalf("Harvest: %v", err)
	}
	if d.maxSeen > 3 {
		t.Fatalf("max in-flight probes = %d, want <= 3", d.maxSeen)
	}
}

type countingDiscoverer struct {
	fakeDiscoverer
	inFlight int32
	maxSeen  int32
	max      chan int32
	mu       sync.Mutex
}

func (c *countingDiscoverer) Probe(ctx context.Context, board string) (bool, error) {
	c.mu.Lock()
	c.inFlight++
	if c.inFlight > c.maxSeen {
		c.maxSeen = c.inFlight
	}
	c.mu.Unlock()
	time.Sleep(5 * time.Millisecond)
	c.mu.Lock()
	c.inFlight--
	c.mu.Unlock()
	return true, nil
}

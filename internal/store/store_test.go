package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/saiwrd/connie/ats"
)

var (
	run1 = time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	run2 = time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	run3 = time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func job(board, id, title string) Row {
	return Row{Job: ats.Job{
		ATS: "ashby", Board: board, ExternalID: id,
		Title: title, Location: "Remote", Remote: true,
		URL: "https://jobs.example/" + id,
	}}
}

func covered(boards ...string) []BoardRef {
	refs := make([]BoardRef, 0, len(boards))
	for _, b := range boards {
		refs = append(refs, BoardRef{ATS: "ashby", Board: b})
	}
	return refs
}

func mustUpsert(t *testing.T, s *Store, rows []Row, at time.Time, cov []BoardRef) Result {
	t.Helper()
	res, err := s.Upsert(rows, at, cov)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	return res
}

func get(t *testing.T, s *Store, key string) StoredRow {
	t.Helper()
	rows, err := s.Query(QueryOptions{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	for _, r := range rows {
		if r.Key() == key {
			return r
		}
	}
	t.Fatalf("key %q not in store", key)
	return StoredRow{}
}

func TestUpsertPreservesHistory(t *testing.T) {
	t.Parallel()
	s := testStore(t)

	res := mustUpsert(t, s, []Row{job("x", "1", "Software Engineer")}, run1, covered("x"))
	if res.New != 1 || res.Updated != 0 {
		t.Fatalf("run1 Result = %+v, want New=1", res)
	}
	if len(res.NewKeys) != 1 || res.NewKeys[0] != "ashby:x:1" {
		t.Fatalf("NewKeys = %v", res.NewKeys)
	}

	// Second sighting with a retitled posting: first_seen survives,
	// last_seen and title refresh, and it counts as Updated not New.
	res = mustUpsert(t, s, []Row{job("x", "1", "Senior Software Engineer")}, run2, covered("x"))
	if res.New != 0 || res.Updated != 1 || len(res.NewKeys) != 0 {
		t.Fatalf("run2 Result = %+v, want Updated=1", res)
	}
	r := get(t, s, "ashby:x:1")
	if !r.FirstSeen.Equal(run1) {
		t.Errorf("FirstSeen = %v, want %v (must survive re-upsert)", r.FirstSeen, run1)
	}
	if !r.LastSeen.Equal(run2) {
		t.Errorf("LastSeen = %v, want %v", r.LastSeen, run2)
	}
	if r.Title != "Senior Software Engineer" {
		t.Errorf("Title = %q, not refreshed", r.Title)
	}
}

func TestMatchedContextSurvivesRunsWithoutGrep(t *testing.T) {
	t.Parallel()
	s := testStore(t)

	withCtx := job("x", "1", "SRE")
	withCtx.Matched = "…we run kubernetes at scale…"
	mustUpsert(t, s, []Row{withCtx}, run1, nil)

	// A later run without grep context must not blank the stored context.
	mustUpsert(t, s, []Row{job("x", "1", "SRE")}, run2, nil)
	if got := get(t, s, "ashby:x:1").Matched; got != "…we run kubernetes at scale…" {
		t.Fatalf("Matched = %q, want preserved context", got)
	}

	// But a run WITH new context replaces it.
	newer := job("x", "1", "SRE")
	newer.Matched = "…now on nomad…"
	mustUpsert(t, s, []Row{newer}, run3, nil)
	if got := get(t, s, "ashby:x:1").Matched; got != "…now on nomad…" {
		t.Fatalf("Matched = %q, want replaced context", got)
	}
}

func TestUnfilteredRunClosesVanishedPostings(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	mustUpsert(t, s, []Row{job("x", "1", "A"), job("x", "2", "B")}, run1, covered("x"))

	// Job 2 vanished from a fully-covered scan of board x.
	res := mustUpsert(t, s, []Row{job("x", "1", "A")}, run2, covered("x"))
	if res.Closed != 1 {
		t.Fatalf("Closed = %d, want 1", res.Closed)
	}
	if got := get(t, s, "ashby:x:2"); got.ClosedAt == nil || !got.ClosedAt.Equal(run2) {
		t.Fatalf("ClosedAt = %v, want %v", got.ClosedAt, run2)
	}
	if got := get(t, s, "ashby:x:1"); got.ClosedAt != nil {
		t.Fatalf("still-listed job closed: %v", got.ClosedAt)
	}
}

func TestFilteredRunNeverClosesAnything(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	mustUpsert(t, s, []Row{job("x", "1", "A"), job("x", "2", "B")}, run1, covered("x"))

	// covered=nil marks a filtered run: absence proves nothing.
	res := mustUpsert(t, s, []Row{job("x", "1", "A")}, run2, nil)
	if res.Closed != 0 {
		t.Fatalf("Closed = %d, want 0 on filtered run", res.Closed)
	}
	if got := get(t, s, "ashby:x:2"); got.ClosedAt != nil {
		t.Fatalf("filtered run closed a posting: %v", got.ClosedAt)
	}
}

func TestClosingScopedToBoardsActuallyScanned(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	mustUpsert(t, s, []Row{job("x", "1", "A"), job("y", "2", "B")}, run1, covered("x", "y"))

	// Run 2 fully covers board x only. Board y's job is absent but y was
	// not scanned — it must survive.
	res := mustUpsert(t, s, []Row{job("x", "1", "A")}, run2, covered("x"))
	if res.Closed != 0 {
		t.Fatalf("Closed = %d, want 0", res.Closed)
	}
	if got := get(t, s, "ashby:y:2"); got.ClosedAt != nil {
		t.Fatalf("unscanned board's job closed: %v", got.ClosedAt)
	}
}

func TestReappearanceClearsClosedAt(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	mustUpsert(t, s, []Row{job("x", "1", "A")}, run1, covered("x"))
	mustUpsert(t, s, []Row{}, run2, covered("x")) // vanishes → closed
	if got := get(t, s, "ashby:x:1"); got.ClosedAt == nil {
		t.Fatal("setup: job not closed")
	}

	// The posting comes back: it must reopen.
	res := mustUpsert(t, s, []Row{job("x", "1", "A")}, run3, covered("x"))
	if res.Updated != 1 {
		t.Fatalf("reappearance Result = %+v, want Updated=1", res)
	}
	if got := get(t, s, "ashby:x:1"); got.ClosedAt != nil {
		t.Fatalf("ClosedAt = %v, want cleared on reappearance", got.ClosedAt)
	}
}

func TestQueryFilters(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	mustUpsert(t, s, []Row{job("x", "1", "Old Role"), job("x", "2", "Doomed Role")}, run1, covered("x"))
	mustUpsert(t, s, []Row{job("x", "1", "Old Role"), job("x", "3", "Fresh Role")}, run2, covered("x")) // 2 closes, 3 is new

	all, err := s.Query(QueryOptions{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("Query(all) = %d rows, want 3", len(all))
	}

	open, err := s.Query(QueryOptions{OpenOnly: true})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(open) != 2 {
		t.Fatalf("Query(open) = %d rows, want 2 (closed excluded)", len(open))
	}

	fresh, err := s.Query(QueryOptions{FirstSeenAfter: run1})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(fresh) != 1 || fresh[0].ExternalID != "3" {
		t.Fatalf("Query(FirstSeenAfter run1) = %+v, want just job 3", fresh)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "registry.db")
	s1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	mustUpsert(t, s1, []Row{job("x", "1", "A")}, run1, nil)
	s1.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s2.Close()
	rows, err := s2.Query(QueryOptions{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("reopened store lost data: %d rows", len(rows))
	}
}

func TestEmptyUpsertIsSafe(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	res, err := s.Upsert(nil, run1, nil)
	if err != nil {
		t.Fatalf("Upsert(nil): %v", err)
	}
	if res.New+res.Updated+res.Closed != 0 {
		t.Fatalf("Upsert(nil) Result = %+v, want zeros", res)
	}
}

func TestOpenBadPathErrors(t *testing.T) {
	t.Parallel()
	if _, err := Open(filepath.Join(t.TempDir(), "no", "such", "dir", "x.db")); err == nil {
		t.Fatal("Open() error = nil, want error for unreachable path")
	}
}

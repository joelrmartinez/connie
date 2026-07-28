package scan

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/saiwrd/connie/internal/ats"
	"github.com/saiwrd/connie/internal/filter"
)

// fakeAdapter simulates an ATS with per-board behavior and instruments
// concurrency: it records every board fetched and the max in-flight fetches.
type fakeAdapter struct {
	name    string
	delay   time.Duration
	dead    map[string]bool // boards that 404
	broken  map[string]bool // boards that error non-404, persistently
	jobsFor func(board string) []ats.Job

	mu       sync.Mutex
	fetched  []string
	inFlight int32
	maxSeen  int32
}

func (f *fakeAdapter) Name() string { return f.name }

func (f *fakeAdapter) Fetch(ctx context.Context, board string, _ ats.FetchOptions) ([]ats.Job, error) {
	cur := atomic.AddInt32(&f.inFlight, 1)
	for {
		prev := atomic.LoadInt32(&f.maxSeen)
		if cur <= prev || atomic.CompareAndSwapInt32(&f.maxSeen, prev, cur) {
			break
		}
	}
	defer atomic.AddInt32(&f.inFlight, -1)

	f.mu.Lock()
	f.fetched = append(f.fetched, board)
	f.mu.Unlock()

	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.dead[board] {
		return nil, fmt.Errorf("%s board %q: %w", f.name, board, ats.ErrBoardNotFound)
	}
	if f.broken[board] {
		return nil, errors.New("transient explosion")
	}
	if f.jobsFor != nil {
		return f.jobsFor(board), nil
	}
	return []ats.Job{{ATS: f.name, Board: board, ExternalID: board + "-1", Title: "Engineer " + board}}, nil
}

func boards(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("board-%02d", i)
	}
	return out
}

func tasksFor(a ats.Adapter, names []string) []Task {
	t := make([]Task, 0, len(names))
	for _, n := range names {
		t = append(t, Task{Adapter: a, Board: n})
	}
	return t
}

func unfiltered(t *testing.T) *filter.Filter {
	t.Helper()
	f, err := filter.New()
	if err != nil {
		t.Fatalf("filter.New: %v", err)
	}
	return f
}

func TestRunVisitsEveryBoardExactlyOnce(t *testing.T) {
	t.Parallel()
	fake := &fakeAdapter{name: "ashby"}
	names := boards(30)

	rep, err := Run(context.Background(), tasksFor(fake, names), unfiltered(t), Config{Concurrency: 4})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fake.fetched) != len(names) {
		t.Fatalf("fetched %d boards, want %d", len(fake.fetched), len(names))
	}
	seen := map[string]int{}
	for _, b := range fake.fetched {
		seen[b]++
	}
	for _, n := range names {
		if seen[n] != 1 {
			t.Errorf("board %s fetched %d times, want 1", n, seen[n])
		}
	}
	if len(rep.Rows) != len(names) {
		t.Fatalf("rows = %d, want %d", len(rep.Rows), len(names))
	}
}

func TestRunBoundsConcurrency(t *testing.T) {
	t.Parallel()
	fake := &fakeAdapter{name: "ashby", delay: 20 * time.Millisecond}
	_, err := Run(context.Background(), tasksFor(fake, boards(24)), unfiltered(t), Config{Concurrency: 3})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if max := atomic.LoadInt32(&fake.maxSeen); max > 3 {
		t.Fatalf("max in-flight = %d, want <= 3", max)
	}
	if max := atomic.LoadInt32(&fake.maxSeen); max < 2 {
		t.Fatalf("max in-flight = %d — pool not actually parallel", max)
	}
}

func TestRunDeadBoardsLandInDeadNotErrs(t *testing.T) {
	t.Parallel()
	fake := &fakeAdapter{name: "ashby", dead: map[string]bool{"board-03": true}}
	rep, err := Run(context.Background(), tasksFor(fake, boards(6)), unfiltered(t), Config{Concurrency: 2})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rep.Dead) != 1 || rep.Dead[0].Board != "board-03" {
		t.Fatalf("Dead = %+v, want just board-03", rep.Dead)
	}
	if rep.Errs != 0 {
		t.Fatalf("Errs = %d, want 0 (404 is a fact, not a fault)", rep.Errs)
	}
	if len(rep.Rows) != 5 {
		t.Fatalf("rows = %d, want 5", len(rep.Rows))
	}
}

func TestRunOneBrokenBoardDoesNotStopTheScan(t *testing.T) {
	t.Parallel()
	fake := &fakeAdapter{name: "ashby", broken: map[string]bool{"board-01": true}}
	rep, err := Run(context.Background(), tasksFor(fake, boards(8)), unfiltered(t), Config{Concurrency: 2})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.Errs != 1 {
		t.Fatalf("Errs = %d, want 1", rep.Errs)
	}
	if len(rep.Rows) != 7 {
		t.Fatalf("rows = %d, want 7 — the other boards must complete", len(rep.Rows))
	}
}

func TestRunCoveredOnlyWhenUnfiltered(t *testing.T) {
	t.Parallel()

	// Unfiltered run: every successfully scanned board is covered; the dead
	// board is not (no proof its postings vanished vs. the board moving).
	fake := &fakeAdapter{name: "ashby", dead: map[string]bool{"board-02": true}}
	rep, err := Run(context.Background(), tasksFor(fake, boards(4)), unfiltered(t), Config{Concurrency: 2})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rep.Covered) != 3 {
		t.Fatalf("Covered = %d boards, want 3 (dead board excluded)", len(rep.Covered))
	}

	// Filtered run: Covered must be nil so the store closes nothing.
	titled, err := filter.New(filter.WithTitle("engineer", false))
	if err != nil {
		t.Fatalf("filter.New: %v", err)
	}
	fake2 := &fakeAdapter{name: "ashby"}
	rep, err = Run(context.Background(), tasksFor(fake2, boards(4)), titled, Config{Concurrency: 2})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.Covered != nil {
		t.Fatalf("Covered = %+v on a filtered run, want nil", rep.Covered)
	}
}

func TestRunAppliesFilter(t *testing.T) {
	t.Parallel()
	fake := &fakeAdapter{name: "ashby", jobsFor: func(board string) []ats.Job {
		return []ats.Job{
			{ATS: "ashby", Board: board, ExternalID: "1", Title: "Platform Engineer", Remote: true},
			{ATS: "ashby", Board: board, ExternalID: "2", Title: "Accountant", Remote: false},
		}
	}}
	f, err := filter.New(filter.WithTitle("platform engineer", false), filter.WithRemoteOnly())
	if err != nil {
		t.Fatalf("filter.New: %v", err)
	}
	rep, err := Run(context.Background(), tasksFor(fake, boards(3)), f, Config{Concurrency: 2})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rep.Rows) != 3 {
		t.Fatalf("rows = %d, want 3 (one match per board)", len(rep.Rows))
	}
	for _, r := range rep.Rows {
		if r.Title != "Platform Engineer" {
			t.Fatalf("unexpected row through filter: %+v", r)
		}
	}
}

func TestRunCancellationDrainsAndInvalidatesCoverage(t *testing.T) {
	t.Parallel()
	fake := &fakeAdapter{name: "ashby", delay: 30 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(45 * time.Millisecond) // let a couple of boards finish
		cancel()
	}()

	start := time.Now()
	rep, err := Run(ctx, tasksFor(fake, boards(50)), unfiltered(t), Config{Concurrency: 2})
	if err == nil {
		t.Fatal("Run() error = nil, want context error after cancel")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Run took %v after cancel — workers not draining", elapsed)
	}
	if rep.Covered != nil {
		t.Fatal("Covered non-nil after cancellation — partial coverage must not close postings")
	}
	if len(fake.fetched) == 50 {
		t.Fatal("all boards fetched despite cancellation")
	}
}

func TestRunProgressCallback(t *testing.T) {
	t.Parallel()
	fake := &fakeAdapter{name: "ashby"}
	var mu sync.Mutex
	var calls int
	cfg := Config{Concurrency: 2, Progress: func(done, total int) {
		mu.Lock()
		calls++
		mu.Unlock()
		if total != 5 {
			t.Errorf("Progress total = %d, want 5", total)
		}
	}}
	if _, err := Run(context.Background(), tasksFor(fake, boards(5)), unfiltered(t), cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 5 {
		t.Fatalf("Progress called %d times, want once per board", calls)
	}
}

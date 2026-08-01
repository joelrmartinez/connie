// Package scan runs the joes: a bounded worker pool that fetches thousands
// of boards concurrently, applies the filter, and aggregates results over
// channels — a single aggregator owns all mutable state, so there isn't a
// mutex in the package and `go test -race` proves the design.
//
// Deliberately not errgroup: its cancel-on-first-error semantics are wrong
// here — one dead board must never abort a 3,600-board scan.
package scan

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/saiwrd/connie/ats"
	"github.com/saiwrd/connie/internal/filter"
	"github.com/saiwrd/connie/internal/store"
)

// Task names one board on one ATS.
type Task struct {
	Adapter ats.Adapter
	Board   string
}

// Config tunes a scan.
type Config struct {
	// Concurrency caps in-flight fetches. It is a politeness cap, not a
	// performance ceiling; default 8.
	Concurrency int
	// PerBoardTimeout bounds each board fetch; default 30s.
	PerBoardTimeout time.Duration
	// Progress, when set, is called after each board completes.
	Progress func(done, total int)
}

// Report is the outcome of a scan.
type Report struct {
	Rows []store.Row // filtered postings, ready for the Registry
	Dead []Task      // boards that 404ed — prune them from the cache
	Errs int         // boards that failed for any other reason
	// Covered lists boards scanned exhaustively and proven live by at least
	// one posting; nil on filtered or canceled runs, in which case the
	// Registry closes nothing. A board that returned zero postings is never
	// covered: empty is not proof.
	Covered []store.BoardRef
}

// result is one worker's verdict on one task.
type result struct {
	task Task
	rows []store.Row
	dead bool
	err  error
}

// Run scans every task through a bounded pool and aggregates the report.
// On context cancellation it returns promptly with partial Rows, a nil
// Covered (partial coverage must not close postings), and the ctx error.
func Run(ctx context.Context, tasks []Task, f *filter.Filter, cfg Config) (Report, error) {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 8
	}
	if cfg.PerBoardTimeout <= 0 {
		cfg.PerBoardTimeout = 30 * time.Second
	}
	opt := ats.FetchOptions{Descriptions: f.NeedsDescriptions()}

	feed := make(chan Task)
	results := make(chan result)

	// Producer: stops feeding as soon as the context dies.
	go func() {
		defer close(feed)
		for _, t := range tasks {
			select {
			case feed <- t:
			case <-ctx.Done():
				return
			}
		}
	}()

	// The joes. Each owns nothing but the task in its hands.
	done := make(chan struct{})
	for i := 0; i < cfg.Concurrency; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for t := range feed {
				results <- work(ctx, t, f, opt, cfg.PerBoardTimeout)
			}
		}()
	}
	go func() {
		for i := 0; i < cfg.Concurrency; i++ {
			<-done
		}
		close(results)
	}()

	// Aggregator: the only goroutine that touches the report.
	var (
		rep       Report
		covered   []store.BoardRef
		completed int
	)
	for r := range results {
		completed++
		switch {
		case r.dead:
			rep.Dead = append(rep.Dead, r.task)
		case r.err != nil:
			rep.Errs++
		default:
			rep.Rows = append(rep.Rows, r.rows...)
			// A board that returned nothing proves nothing. A 200 with an
			// empty list is indistinguishable from a dead slug, a lapsed
			// subscription, or a changed response shape, so covering it
			// would let the Registry close the board's whole history on no
			// evidence. Only a board that actually produced postings is
			// proof enough to close the ones it no longer lists. The cost
			// is that a board which genuinely empties out keeps its last
			// postings open; that is the cheaper mistake by far.
			if len(r.rows) > 0 {
				covered = append(covered, store.BoardRef{ATS: r.task.Adapter.Name(), Board: r.task.Board})
			}
		}
		if cfg.Progress != nil {
			cfg.Progress(completed, len(tasks))
		}
	}

	if err := ctx.Err(); err != nil {
		return rep, fmt.Errorf("scan interrupted after %d/%d boards: %w", completed, len(tasks), err)
	}
	if f.Unfiltered() {
		rep.Covered = covered
	}
	return rep, nil
}

// work fetches and filters one board. A non-404 failure gets one retry —
// the httpx client has already retried transport-level faults underneath.
func work(ctx context.Context, t Task, f *filter.Filter, opt ats.FetchOptions, timeout time.Duration) result {
	jobs, err := fetchOnce(ctx, t, opt, timeout)
	if err != nil && !errors.Is(err, ats.ErrBoardNotFound) && ctx.Err() == nil {
		jobs, err = fetchOnce(ctx, t, opt, timeout)
	}
	if err != nil {
		return result{task: t, dead: errors.Is(err, ats.ErrBoardNotFound), err: err}
	}

	rows := make([]store.Row, 0, len(jobs))
	for _, j := range jobs {
		ok, frags := f.Match(j)
		if !ok {
			continue
		}
		row := store.Row{Job: j}
		if len(frags) > 0 {
			row.Matched = frags[0]
			for _, fr := range frags[1:] {
				row.Matched += " … " + fr
			}
		}
		row.Description = "" // fragments survive; the full text never does
		rows = append(rows, row)
	}
	return result{task: t, rows: rows}
}

func fetchOnce(ctx context.Context, t Task, opt ats.FetchOptions, timeout time.Duration) ([]ats.Job, error) {
	fctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return t.Adapter.Fetch(fctx, t.Board, opt)
}

// Package cli wires connie's subcommands: scan, discover, query, version.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/saiwrd/connie/ats"
	"github.com/saiwrd/connie/ats/ashby"
	"github.com/saiwrd/connie/ats/greenhouse"
	"github.com/saiwrd/connie/ats/lever"
	"github.com/saiwrd/connie/ats/workable"
	"github.com/saiwrd/connie/internal/discover"
	"github.com/saiwrd/connie/internal/export"
	"github.com/saiwrd/connie/internal/filter"
	"github.com/saiwrd/connie/internal/httpx"
	"github.com/saiwrd/connie/internal/scan"
	"github.com/saiwrd/connie/internal/store"
)

const version = "connie 0.1.0"

// Test seams. Production leaves these at their defaults.
var (
	stdout        io.Writer         = os.Stdout
	stderr        io.Writer         = os.Stderr
	testTransport http.RoundTripper = nil
)

const usageText = `connie — the Circus's memory for public job boards

Usage:
  connie scan     --title Q | --grep RE | --all   [flags]   scan boards, filter, export, remember
  connie discover [--ats ...]                     [flags]   harvest board slugs from the Wayback CDX
  connie query    [--open] [--since 24h] [flags]            read the Registry offline
  connie version

Etiquette: set CONNIE_CONTACT to a real address; it goes in the User-Agent.
`

// Run dispatches args and returns the process exit code.
func Run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return 2
	}
	switch args[0] {
	case "scan":
		return runScan(args[1:])
	case "discover":
		return runDiscover(args[1:])
	case "query":
		return runQuery(args[1:])
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	default:
		fmt.Fprintf(stderr, "connie: unknown command %q\n%s", args[0], usageText)
		return 2
	}
}

func fail(err error) int {
	fmt.Fprintf(stderr, "connie: %v\n", err)
	return 1
}

func userAgent() string {
	ua := "connie/0.1 (+https://github.com/saiwrd/connie)"
	if contact := os.Getenv("CONNIE_CONTACT"); contact != "" {
		ua = fmt.Sprintf("connie/0.1 (+https://github.com/saiwrd/connie; contact: %s)", contact)
	}
	return ua
}

func newClient(extra ...httpx.Option) *httpx.Client {
	opts := append([]httpx.Option{httpx.WithUserAgent(userAgent())}, extra...)
	if testTransport != nil {
		opts = append(opts, httpx.WithBaseTransport(testTransport))
	}
	return httpx.New(opts...)
}

// adaptersFor resolves --ats into adapter instances, seed/cache aware.
func adaptersFor(names string, client ats.Getter) ([]ats.Adapter, error) {
	all := map[string]ats.Adapter{
		"ashby":      ashby.New(client),
		"greenhouse": greenhouse.New(client),
		"lever":      lever.New(client),
		"workable":   workable.New(client),
	}
	var out []ats.Adapter
	for _, n := range strings.Split(names, ",") {
		n = strings.TrimSpace(strings.ToLower(n))
		if n == "" {
			continue
		}
		a, ok := all[n]
		if !ok {
			return nil, fmt.Errorf("unknown ats %q (have: ashby, greenhouse, lever, workable)", n)
		}
		out = append(out, a)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no ats selected")
	}
	return out, nil
}

func boardsFor(name string) ([]string, error) {
	seed, err := discover.Load(filepath.Join("seeds", name+".json"))
	if err != nil {
		return nil, err
	}
	cache, err := discover.Load(filepath.Join("boards", name+".json"))
	if err != nil {
		return nil, err
	}
	boards := discover.Union(seed, cache)
	if len(boards) == 0 {
		return nil, fmt.Errorf("no boards for %s: seed seeds/%s.json missing or empty (run `connie discover`?)", name, name)
	}
	return boards, nil
}

func runScan(args []string) int {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		atsList     = fs.String("ats", "ashby,greenhouse,lever,workable", "comma-separated ATSes to scan")
		title       = fs.String("title", "", "title to match (fuzzy)")
		exact       = fs.Bool("exact", false, "match --title exactly")
		grep        = fs.String("grep", "", "case-insensitive regex over descriptions")
		remote      = fs.Bool("remote", false, "remote postings only")
		all         = fs.Bool("all", false, "no filters: every listed posting")
		limit       = fs.Int("limit", 0, "scan at most N boards per ATS (testing; disables closing)")
		concurrency = fs.Int("concurrency", 8, "max in-flight fetches")
		timeout     = fs.Duration("timeout", 30*time.Second, "per-board timeout")
		outPrefix   = fs.String("out", "connie-jobs", "output file prefix (.csv/.json)")
		dbPath      = fs.String("db", "connie.db", "Registry path")
		noDB        = fs.Bool("no-db", false, "skip the Registry")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// Invariants ported from the reference.
	if *all && (*title != "" || *grep != "") {
		return fail(fmt.Errorf("--all conflicts with --title/--grep"))
	}
	if !*all && *title == "" && *grep == "" {
		return fail(fmt.Errorf("choose a mode: --title, --grep, or --all"))
	}
	if *grep != "" && !strings.Contains(*grep, `\b`) {
		fmt.Fprintf(stderr, "note: pattern %q has no \\b word boundaries — substring matches can be noisy\n", *grep)
	}

	var fopts []filter.Option
	if *title != "" {
		fopts = append(fopts, filter.WithTitle(*title, *exact))
	}
	if *grep != "" {
		fopts = append(fopts, filter.WithGrep(*grep))
	}
	if *remote {
		fopts = append(fopts, filter.WithRemoteOnly())
	}
	f, err := filter.New(fopts...)
	if err != nil {
		return fail(err)
	}

	client := newClient()
	adapters, err := adaptersFor(*atsList, client)
	if err != nil {
		return fail(err)
	}

	var tasks []scan.Task
	for _, a := range adapters {
		boards, err := boardsFor(a.Name())
		if err != nil {
			return fail(err)
		}
		if *limit > 0 && len(boards) > *limit {
			boards = boards[:*limit]
		}
		for _, b := range boards {
			tasks = append(tasks, scan.Task{Adapter: a, Board: b})
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cfg := scan.Config{
		Concurrency:     *concurrency,
		PerBoardTimeout: *timeout,
		Progress: func(done, total int) {
			if done%100 == 0 || done == total {
				fmt.Fprintf(stderr, "  %d/%d boards\n", done, total)
			}
		},
	}
	rep, scanErr := scan.Run(ctx, tasks, f, cfg)
	if scanErr != nil {
		fmt.Fprintf(stderr, "connie: %v — writing partial results\n", scanErr)
	}
	// A limited run never covers a board list exhaustively.
	if *limit > 0 {
		rep.Covered = nil
	}

	if err := writeExports(*outPrefix, rep.Rows); err != nil {
		return fail(err)
	}

	if !*noDB {
		s, err := store.Open(*dbPath)
		if err != nil {
			return fail(err)
		}
		defer s.Close()
		res, err := s.Upsert(rep.Rows, time.Now().UTC(), rep.Covered)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(stderr, "registry: %d new, %d updated, %d closed (%s)\n",
			res.New, res.Updated, res.Closed, *dbPath)
	}

	pruneDead(rep.Dead)

	fmt.Fprintf(stderr, "%d postings -> %s.csv, %s.json | %d dead boards | %d errors\n",
		len(rep.Rows), *outPrefix, *outPrefix, len(rep.Dead), rep.Errs)
	if scanErr != nil {
		return 1
	}
	return 0
}

func writeExports(prefix string, rows []store.Row) error {
	csvFile, err := os.Create(prefix + ".csv")
	if err != nil {
		return fmt.Errorf("create csv: %w", err)
	}
	defer csvFile.Close()
	if err := export.CSV(csvFile, rows); err != nil {
		return err
	}
	jsonFile, err := os.Create(prefix + ".json")
	if err != nil {
		return fmt.Errorf("create json: %w", err)
	}
	defer jsonFile.Close()
	return export.JSON(jsonFile, rows)
}

// pruneDead removes 404'd boards from discovery caches — never from seeds.
func pruneDead(dead []scan.Task) {
	byATS := map[string]map[string]bool{}
	for _, t := range dead {
		if byATS[t.Adapter.Name()] == nil {
			byATS[t.Adapter.Name()] = map[string]bool{}
		}
		byATS[t.Adapter.Name()][strings.ToLower(t.Board)] = true
	}
	for name, gone := range byATS {
		path := filepath.Join("boards", name+".json")
		cache, err := discover.Load(path)
		if err != nil || cache == nil {
			continue // no cache to prune
		}
		var kept []string
		for _, b := range cache {
			if !gone[strings.ToLower(b)] {
				kept = append(kept, b)
			}
		}
		if len(kept) != len(cache) {
			if err := discover.Save(path, kept); err != nil {
				fmt.Fprintf(stderr, "note: could not prune %s: %v\n", path, err)
			}
		}
	}
}

func runDiscover(args []string) int {
	fs := flag.NewFlagSet("discover", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		atsList     = fs.String("ats", "ashby,greenhouse,lever,workable", "comma-separated ATSes to discover")
		concurrency = fs.Int("concurrency", 8, "max in-flight probes")
		cdxTimeout  = fs.Duration("cdx-timeout", 5*time.Minute, "timeout for one archive query")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// A CDX response can be tens of megabytes and take minutes to stream. The
	// 30s default that suits a board fetch silently turned discovery into a
	// timeout, which read as "the archive has nothing" rather than "we hung
	// up first".
	client := newClient(httpx.WithTimeout(*cdxTimeout))
	adapters, err := adaptersFor(*atsList, client)
	if err != nil {
		return fail(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	for _, a := range adapters {
		d, ok := a.(ats.Discoverer)
		if !ok {
			fmt.Fprintf(stderr, "%s: no discovery support; relying on seeds\n", a.Name())
			continue
		}
		fmt.Fprintf(stderr, "%s: querying the Wayback CDX...\n", a.Name())
		live, err := discover.Harvest(ctx, d, client, discover.Config{
			Concurrency: *concurrency,
			Progress: func(done, total int) {
				if done%250 == 0 || done == total {
					fmt.Fprintf(stderr, "  %d/%d candidates probed\n", done, total)
				}
			},
		})
		if err != nil {
			return fail(err)
		}

		path := filepath.Join("boards", a.Name()+".json")
		seed, _ := discover.Load(filepath.Join("seeds", a.Name()+".json"))
		cache, _ := discover.Load(path)
		merged := discover.Union(seed, cache, live)
		if err := discover.Save(path, merged); err != nil {
			return fail(err)
		}
		fmt.Fprintf(stderr, "%s: %d live found, %d total known -> %s\n", a.Name(), len(live), len(merged), path)
	}
	return 0
}

func runQuery(args []string) int {
	fs := flag.NewFlagSet("query", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		dbPath = fs.String("db", "connie.db", "Registry path")
		open   = fs.Bool("open", false, "open postings only")
		since  = fs.Duration("since", 0, "first seen within this window (e.g. 24h)")
		title  = fs.String("title", "", "title to match (fuzzy)")
		remote = fs.Bool("remote", false, "remote postings only")
		format = fs.String("format", "table", "output: table, csv, json")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	s, err := store.Open(*dbPath)
	if err != nil {
		return fail(err)
	}
	defer s.Close()

	opts := store.QueryOptions{OpenOnly: *open}
	if *since > 0 {
		opts.FirstSeenAfter = time.Now().UTC().Add(-*since)
	}
	rows, err := s.Query(opts)
	if err != nil {
		return fail(err)
	}

	var out []store.Row
	for _, r := range rows {
		if *title != "" && !filter.TitleMatches(r.Title, *title, false) {
			continue
		}
		if *remote && !r.Remote {
			continue
		}
		out = append(out, store.Row{Job: r.Job, Matched: r.Matched})
	}

	switch *format {
	case "csv":
		return exitOn(export.CSV(stdout, out))
	case "json":
		return exitOn(export.JSON(stdout, out))
	case "table":
		for _, r := range out {
			fmt.Fprintf(stdout, "%-11s %-24s %-52s %s\n", r.ATS, truncate(r.Board, 24), truncate(r.Title, 52), r.URL)
		}
		fmt.Fprintf(stderr, "%d postings\n", len(out))
		return 0
	default:
		return fail(fmt.Errorf("unknown format %q", *format))
	}
}

func exitOn(err error) int {
	if err != nil {
		return fail(err)
	}
	return 0
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

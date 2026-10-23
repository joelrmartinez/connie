// Package cli wires connie's subcommands: init, scan, discover, query, version.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/joelrmartinez/connie/ats"
	"github.com/joelrmartinez/connie/ats/ashby"
	"github.com/joelrmartinez/connie/ats/greenhouse"
	"github.com/joelrmartinez/connie/ats/lever"
	"github.com/joelrmartinez/connie/ats/workable"
	"github.com/joelrmartinez/connie/internal/config"
	"github.com/joelrmartinez/connie/internal/discover"
	"github.com/joelrmartinez/connie/internal/export"
	"github.com/joelrmartinez/connie/internal/filter"
	"github.com/joelrmartinez/connie/internal/httpx"
	"github.com/joelrmartinez/connie/internal/place"
	"github.com/joelrmartinez/connie/internal/rank"
	"github.com/joelrmartinez/connie/internal/scan"
	"github.com/joelrmartinez/connie/internal/store"
	"github.com/joelrmartinez/connie/internal/version"
	"github.com/joelrmartinez/connie/seeds"
)

// Test seams. Production leaves these at their defaults.
var (
	stdout        io.Writer         = os.Stdout
	stderr        io.Writer         = os.Stderr
	testTransport http.RoundTripper = nil
)

const usageText = `connie: finds matching jobs on public company job boards

Usage:
  connie init     [--defaults] [--out connie.yaml]          write a saved search (asks, or takes defaults)
  connie scan     --title Q | --grep RE | --all   [flags]   scan boards, filter, export, remember
                  (no mode flag: use the saved search in connie.yaml)
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
	case "init":
		return runInit(args[1:])
	case "scan":
		return runScan(args[1:])
	case "discover":
		return runDiscover(args[1:])
	case "query":
		return runQuery(args[1:])
	case "version":
		fmt.Fprintln(stdout, "connie", version.Version)
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
	return version.UserAgent(os.Getenv("CONNIE_CONTACT"))
}

const contactWarning = "connie: warning: CONNIE_CONTACT is not set; set it to a real address so ATS operators can reach you (it goes in the User-Agent)\n"

func newClient(extra ...httpx.Option) *httpx.Client {
	if os.Getenv("CONNIE_CONTACT") == "" {
		fmt.Fprint(stderr, contactWarning)
	}
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

// defaultSeedsDir is checked for local seed overrides when --seeds is unset.
const defaultSeedsDir = "seeds"

// loadSeed returns the seed list for one ATS. An explicit dir always wins;
// otherwise a local seeds/ directory overrides the seeds embedded in the
// binary, so a checkout can edit them and an installed binary still works
// from anywhere.
func loadSeed(name, dir string) ([]string, error) {
	if dir != "" {
		return discover.Load(filepath.Join(dir, name+".json"))
	}
	if fi, err := os.Stat(defaultSeedsDir); err == nil && fi.IsDir() {
		return discover.Load(filepath.Join(defaultSeedsDir, name+".json"))
	}
	body, err := seeds.FS.ReadFile(name + ".json")
	if err != nil {
		return nil, nil
	}
	var boards []string
	if err := json.Unmarshal(body, &boards); err != nil {
		return nil, fmt.Errorf("parse embedded seed %s.json: %w", name, err)
	}
	return boards, nil
}

func boardsFor(name, seedsDir string) ([]string, error) {
	seed, err := loadSeed(name, seedsDir)
	if err != nil {
		return nil, err
	}
	cache, err := discover.Load(filepath.Join("boards", name+".json"))
	if err != nil {
		return nil, err
	}
	boards := discover.Union(seed, cache)
	if len(boards) == 0 {
		return nil, fmt.Errorf("no boards for %s: seed %s.json missing or empty (run `connie discover`?)", name, name)
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
		cfgPath     = fs.String("config", config.DefaultPath, "saved search file; flags override it")
		noConfig    = fs.Bool("no-config", false, "ignore the saved search file")
		hardFilter  = fs.Bool("hard-filter", false, "with a saved search, drop postings outside its titles and locations instead of ranking them")
		top         = fs.Int("top", 10, "with a saved search, print the N best-ranked postings (0 for none)")
		seedsDir    = fs.String("seeds", "", "seed directory (default: ./seeds if present, else built-in seeds)")
		descPay     = fs.Bool("desc-pay", false, "parse pay from description text when the ATS gives none (default from config pay.count_description)")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	set := setFlags(fs)

	var cfg *config.Config
	if !*noConfig {
		var err error
		if cfg, err = loadConfig(*cfgPath, set["config"]); err != nil {
			return fail(err)
		}
	}
	if cfg != nil && !set["desc-pay"] {
		*descPay = cfg.Pay.CountDescription
	}
	if cfg != nil && !set["ats"] && len(cfg.ATS) > 0 {
		*atsList = cfg.ATSList()
	}
	// The saved search supplies the mode only when no mode flag was given:
	// --title, --grep, or --all on the command line replace it entirely.
	useSaved := cfg != nil && len(cfg.Titles) > 0 && !*all && *title == "" && *grep == ""
	if useSaved {
		fmt.Fprintf(stderr, "using saved search %s (%d titles)\n", *cfgPath, len(cfg.Titles))
	}

	// Invariants ported from the reference.
	if *all && (*title != "" || *grep != "") {
		return fail(fmt.Errorf("--all conflicts with --title/--grep"))
	}
	if !useSaved && !*all && *title == "" && *grep == "" {
		return fail(fmt.Errorf("choose a mode: --title, --grep, or --all (or save one with `connie init`)"))
	}
	if *grep != "" && !strings.Contains(*grep, `\b`) {
		fmt.Fprintf(stderr, "note: pattern %q has no \\b word boundaries; substring matches can be noisy\n", *grep)
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
	var ranker *rank.Ranker
	if useSaved {
		ranker = rank.New(cfg)
		loc := cfg.Location
		// Ranking keeps anything with some title fit and lets the score sort
		// it; --hard-filter restores the strict family and location filter.
		fopts = append(fopts, filter.WithPredicate(func(j ats.Job) bool { return ranker.Score(j).Title > 0 }))
		if *hardFilter {
			fopts = append(fopts,
				filter.WithPredicate(func(j ats.Job) bool { return ranker.Score(j).Title == 1 }),
				filter.WithPredicate(func(j ats.Job) bool { return place.Acceptable(place.Judge(j, loc)) }))
		}
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
		boards, err := boardsFor(a.Name(), *seedsDir)
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

	scfg := scan.Config{
		Concurrency:     *concurrency,
		PerBoardTimeout: *timeout,
		DescriptionPay:  *descPay,
		Progress: func(done, total int) {
			if done%100 == 0 || done == total {
				fmt.Fprintf(stderr, "  %d/%d boards\n", done, total)
			}
		},
	}
	rep, scanErr := scan.Run(ctx, tasks, f, scfg)
	if scanErr != nil {
		fmt.Fprintf(stderr, "connie: %v, writing partial results\n", scanErr)
	}
	if ranker != nil {
		for i := range rep.Rows {
			sc := ranker.Score(rep.Rows[i].Job)
			rep.Rows[i].Family, rep.Rows[i].Score, rep.Rows[i].Reason = sc.Family, sc.Total, sc.Reason
		}
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
	fmt.Fprintln(stderr, paySummary(rep.Rows))
	if ranker != nil {
		printTop(export.Sorted(rep.Rows), *top)
	}
	if scanErr != nil {
		return 1
	}
	return 0
}

// printTop writes the n best rows, already sorted, with their reasons.
func printTop(rows []store.Row, n int) {
	if n > len(rows) {
		n = len(rows)
	}
	if n <= 0 {
		return
	}
	fmt.Fprintf(stdout, "top %d of %d by score:\n", n, len(rows))
	for i, r := range rows[:n] {
		fmt.Fprintf(stdout, "%2d. %5.1f  %s @ %s\n      %s\n      %s\n", i+1, r.Score, r.Title, r.Board, r.Reason, r.URL)
	}
}

// paySummary counts how pay was found, so unknown pay is visible at a
// glance rather than only in a CSV column.
func paySummary(rows []store.Row) string {
	var field, parsed, unknown int
	for _, r := range rows {
		switch r.Pay.Source {
		case ats.PayFromField:
			field++
		case ats.PayFromDescription:
			parsed++
		default:
			unknown++
		}
	}
	return fmt.Sprintf("pay: %d from ATS fields, %d parsed from descriptions, %d unknown (flagged, kept)", field, parsed, unknown)
}

// setFlags reports which flags were given explicitly, so a config value
// can fill only the ones left at their defaults.
func setFlags(fs *flag.FlagSet) map[string]bool {
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	return set
}

// loadConfig reads the saved search. The default path may be absent; a
// path the user named explicitly must exist.
func loadConfig(path string, explicit bool) (*config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	if cfg == nil && explicit {
		return nil, fmt.Errorf("config %q not found", path)
	}
	return cfg, nil
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
		seedsDir    = fs.String("seeds", "", "seed directory (default: ./seeds if present, else built-in seeds)")
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
		seed, _ := loadSeed(a.Name(), *seedsDir)
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
			fmt.Fprintf(stdout, "%-11s %-24s %-52s %-30s %s\n", r.ATS, truncate(r.Board, 24), truncate(r.Title, 52), r.Pay, r.URL)
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

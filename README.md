# connie

**Connie finds jobs for you, fast.**

Connie checks thousands of company job boards directly and finds the roles
that match your search in seconds. It remembers every posting it has seen,
so each time you run it you can see only what is new. It ranks results by
how well the title, pay, and location fit what you want. It runs on your own
computer, needs no account, and is free and open source (MIT).

It reads the public job boards that companies host on four hiring systems:
**Ashby, Greenhouse, Lever, and Workable.**

## Quickstart (60 seconds)

**1. Install.** Pick one:

- **Download a binary** (no Go needed): grab the file for your system
  (macOS, Linux, or Windows) from the
  [Releases page](https://github.com/joelrmartinez/connie/releases), unpack
  it, and put `connie` somewhere on your PATH. `checksums.txt` lets you
  verify the download.
- **Or install with Go** (needs Go 1.26 or newer):

  ```sh
  go install github.com/joelrmartinez/connie@latest
  ```

**2. Tell job sites how to reach you.** Connie puts this in every request so
site owners can contact you instead of blocking you. Use a real address.

```sh
export CONNIE_CONTACT="you@example.com"
```

**3. Save a search.** This writes `connie.yaml` with sensible defaults you
can edit later (or run `connie init` to answer a few questions instead).

```sh
connie init --defaults
```

**4. Search.**

```sh
connie scan
```

You get a ranked list in your terminal, plus `connie-jobs.csv` (opens in any
spreadsheet app) and `connie-jobs.json`.

**5. See what is new since yesterday.**

```sh
connie query --open --since 24h --format table
```

Run `connie scan` once a day (by hand or on a schedule) and step 5 becomes a
daily list of fresh openings.

Want a quick one-off search without a saved file?

```sh
connie scan --title "revenue operations"
```

## Supported job boards

| System     | Example board URL                          |
|------------|--------------------------------------------|
| Ashby      | `https://jobs.ashbyhq.com/<company>`       |
| Greenhouse | `https://boards.greenhouse.io/<company>`     |
| Lever      | `https://jobs.lever.co/<company>`          |
| Workable   | `https://apply.workable.com/<company>`     |

Connie ships with a curated list of companies for each system (`seeds/`).
`connie discover` can find many more (see below).

## Glossary

- **Board:** one company's job page on one hiring system (for example,
  `ashby/openai`).
- **Posting:** a single job listing on a board.
- **Seeds:** the built-in list of boards in `seeds/`, checked by hand.
- **Registry:** the local database (`connie.db`) where connie remembers every
  posting, when it first and last saw it, and when it closed.
- **Saved search:** your `connie.yaml` file: titles, locations, pay floor,
  and ranking weights.
- **Dead board:** a board that no longer exists (the site returns "not found").
- **Discover:** an optional step that finds more boards using the Internet
  Archive's Wayback Machine.

The rest of this README is reference and technical detail.

## Commands

```
connie scan     --title Q | --grep RE | --all    scan boards, filter, export, remember
connie discover [--ats ashby,greenhouse,lever,workable]
                                                harvest board slugs from the Wayback Machine
connie query    [--open] [--since 24h]           read the Registry offline
connie version
```

`scan` flags: `--ats`, `--exact`, `--remote`, `--limit N`, `--concurrency` (default 8),
`--timeout` (default 30s), `--out PREFIX`, `--db PATH`, `--no-db`.

- **`--title`** matches fuzzily in both directions: `"software engineer"` finds
  *Senior Software Engineer, Backend*, and a long query still finds the short
  title it contains. The reverse direction requires a multi-word title, so a
  long query can't drag in every posting titled just *Engineer*.
- **`--grep`** is a case-insensitive regex over posting *descriptions* (titles
  are `--title`'s job). Only matched context fragments are kept, full
  descriptions are ~95% of the payload and are never stored. Use `\b`
  boundaries: `--grep 'rust'` also matches *trust*.
- **`--all`** takes every listed posting, and is the only mode allowed to
  mark vanished postings closed (see lifecycle below).

## Saved search (connie.yaml)

Instead of retyping flags, save a search in `connie.yaml` in the directory
you run connie from. Start from a template:

```sh
connie init                        # answer a few questions, writes connie.yaml
connie init --defaults             # no questions (scripts, CI)
cp examples/gtm.yaml connie.yaml   # or start from a template
connie scan                        # no mode flag: uses connie.yaml
```

The file holds titles and their variants, location scope (remote US-only or
anywhere, cities, whether hybrid counts), an optional pay floor, whether pay
found in description text counts, ranking weights, and the ATS list.

Flags always win. `--title`, `--grep`, or `--all` replace the saved titles
and location rules; `--ats` replaces the saved ATS list. `--config PATH`
reads another file and `--no-config` ignores it.

With a saved search, results are ranked rather than filtered. Each posting
with some title fit scores up to `weights.title + weights.pay +
weights.location` (100 by default): title fit (a family or variant match is
full marks, the right words in another order is half), pay (full when at or
above the floor or when no floor is set, proportional below it, half when
unknown), and location (remote US or a listed city with hybrid on is full
marks). CSV and JSON are sorted by score and carry `family`, `score`, and a
`reason` such as `title GTM Engineer, pay unknown, remote US`; the top
`--top N` (default 10) print to stdout. `--hard-filter` drops anything that
is not a family match in an acceptable location, as before.

## Where boards come from

Two files per ATS, by design:

- **`seeds/{ats}.json`**, committed, small, hand-curated and verified live.
  A fresh clone scans real boards with zero network discovery.
- **`boards/{ats}.json`**, gitignored. `connie discover` queries the Wayback
  Machine's CDX index for every archived board URL, shape-filters the noise,
  probes survivors against the live ATS, and unions the result with the seed
  and the previous cache. **The union never shrinks**, and the full crawl
  never turns this repo into a published customer list.

Heads up: Greenhouse's archive footprint is large, a full
`connie discover --ats greenhouse` can surface tens of thousands of
candidates and is best run overnight. Re-runs are safe and resumable by
construction (union semantics).

## The Registry lifecycle

Every posting is keyed `ats:board:external_id` and carries
`first_seen` / `last_seen` / `closed_at`. The rules are deliberately
conservative, and each is pinned by a test:

- Only an **unfiltered** (`--all`) run may close postings, a filtered run
  not seeing a job proves nothing.
- Closing is **scoped to boards the run actually scanned**; `--limit` and
  interrupted runs close nothing.
- A posting that reappears is **reopened**.
- Grep context from an earlier run survives later runs without `--grep`.

`connie query --since 24h --open` is therefore exactly "what's new since
yesterday", the watch-mode primitive.

## Design notes (why it's built this way)

- **Why Go:** a scan is thousands of concurrent HTTP fetches. The pool in
  `internal/scan` is a plain channel worker pool where a single aggregator
  goroutine owns all mutable state, no mutexes in the package, proven by
  `go test -race`.
- **Why not `errgroup`:** its cancel-on-first-error semantics are exactly
  wrong here, one dead board must never abort a 3,600-board scan. Errors
  are counted, not propagated.
- **Why `modernc.org/sqlite`** (the main dependency, alongside a YAML parser): pure Go means
  `CGO_ENABLED=0` and a genuinely static, trivially cross-compiled binary.
  Its write-speed penalty vs the CGO driver is irrelevant at one
  transaction per scan. Cost: transitive `modernc.org/*` packages and a
  bigger binary, a fair trade for `GOOS=linux go build` just working.
- **404 is a fact, not a fault:** `ErrNotFound` never retries, dead boards
  self-prune from the discovery cache (never from seeds), and a failed
  *probe* is tolerated, union semantics mean nothing is lost either way.
- **The User-Agent is always pure ASCII.** An em-dash in a UA header once
  broke a predecessor's latin-1 header encoding. The sanitizer has a
  regression test.
- **Descriptions are fetched only when `--grep` needs them** (Greenhouse's
  `?content=true` roughly doubles the payload), and only the matched
  fragments survive into the Registry.

## Testing

```
make check    # gofmt, go vet, go test -race ./...
```

Everything runs offline against recorded fixtures, trimmed captures of one
real API response per ATS, never full crawls. One live smoke test exists
behind a build tag:

```
CONNIE_CONTACT="you@example.com" go test -tags live ./ats/smoke -v
```

## Etiquette

This scrapes public, unauthenticated APIs that ATS vendors provide so
postings can be shared. Be worthy of that: set `CONNIE_CONTACT`, keep
`--concurrency` at the default 8 (connie also never sends more than 2 requests
at a time to any one site), and don't run discovery in a loop.

## Lineage

The Ashby scan semantics, the fuzzy-match rules, the seed/cache split, the
closed-at lifecycle, are ported from
[mherzog4/ashbyhq-jobs](https://github.com/mherzog4/ashbyhq-jobs), a
single-file Python tool whose README documents six behaviors that look like
bugs and are load-bearing. They survived the rewrite; so did the tests.

## Contributing

Contributions are welcome: bug fixes, new seed boards, better parsing,
docs. See [CONTRIBUTING.md](CONTRIBUTING.md). To report a security issue,
see [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE) © Joel Martinez

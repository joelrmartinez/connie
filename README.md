# connie

**The Circus's memory for public job boards.**

Connie scans the free, unauthenticated APIs of four applicant-tracking
systems — **Ashby, Greenhouse, Lever, and Workable** — filters the postings you care
about, and *remembers every posting she has ever seen* in a local SQLite
Registry: `first_seen`, `last_seen`, and a careful `closed_at` lifecycle.
Run her on a schedule and the diff is a signal feed: **who started hiring
for what, the day it opened.**

Named for Connie Sachs of *Tinker Tailor Soldier Spy* — the researcher whose
head was the Registry. Sibling of [Merlin](https://pushtomain.xyz/work/merlin/)
and [Lamplighter](https://pushtomain.xyz/work/lamplighter/).

```
$ export CONNIE_CONTACT="you@example.com"     # goes in the User-Agent — be a good citizen
$ connie scan --title "revenue operations"
  35/35 boards
registry: 6 new, 0 updated, 0 closed (connie.db)
6 postings -> connie-jobs.csv, connie-jobs.json | 0 dead boards | 0 errors

$ connie query --open --since 24h --format table
ashby   render   Revenue Operations Manager   https://jobs.ashbyhq.com/render/...
```

## Install

```
go install github.com/joelrmartinez/connie@latest
```

Or build from a clone: `make build` — one static binary, `CGO_ENABLED=0`,
cross-compiles from anywhere (`make dist` targets linux/mac, amd64/arm64).

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
  are `--title`'s job). Only matched context fragments are kept — full
  descriptions are ~95% of the payload and are never stored. Use `\b`
  boundaries: `--grep 'rust'` also matches *trust*.
- **`--all`** takes every listed posting, and is the only mode allowed to
  mark vanished postings closed (see lifecycle below).

## Where boards come from

Two files per ATS, by design:

- **`seeds/{ats}.json`** — committed, small, hand-curated and verified live.
  A fresh clone scans real boards with zero network discovery.
- **`boards/{ats}.json`** — gitignored. `connie discover` queries the Wayback
  Machine's CDX index for every archived board URL, shape-filters the noise,
  probes survivors against the live ATS, and unions the result with the seed
  and the previous cache. **The union never shrinks**, and the full crawl
  never turns this repo into a published customer list.

Heads up: Greenhouse's archive footprint is large — a full
`connie discover --ats greenhouse` can surface tens of thousands of
candidates and is best run overnight. Re-runs are safe and resumable by
construction (union semantics).

## The Registry lifecycle

Every posting is keyed `ats:board:external_id` and carries
`first_seen` / `last_seen` / `closed_at`. The rules are deliberately
conservative, and each is pinned by a test:

- Only an **unfiltered** (`--all`) run may close postings — a filtered run
  not seeing a job proves nothing.
- Closing is **scoped to boards the run actually scanned**; `--limit` and
  interrupted runs close nothing.
- A posting that reappears is **reopened**.
- Grep context from an earlier run survives later runs without `--grep`.

`connie query --since 24h --open` is therefore exactly "what's new since
yesterday" — the watch-mode primitive.

## Design notes (why it's built this way)

- **Why Go:** a scan is thousands of concurrent HTTP fetches. The pool in
  `internal/scan` is a plain channel worker pool where a single aggregator
  goroutine owns all mutable state — no mutexes in the package, proven by
  `go test -race`.
- **Why not `errgroup`:** its cancel-on-first-error semantics are exactly
  wrong here — one dead board must never abort a 3,600-board scan. Errors
  are counted, not propagated.
- **Why `modernc.org/sqlite`** (the only direct dependency): pure Go means
  `CGO_ENABLED=0` and a genuinely static, trivially cross-compiled binary.
  Its write-speed penalty vs the CGO driver is irrelevant at one
  transaction per scan. Cost: transitive `modernc.org/*` packages and a
  bigger binary — a fair trade for `GOOS=linux go build` just working.
- **404 is a fact, not a fault:** `ErrNotFound` never retries, dead boards
  self-prune from the discovery cache (never from seeds), and a failed
  *probe* is tolerated — union semantics mean nothing is lost either way.
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

Everything runs offline against recorded fixtures — trimmed captures of one
real API response per ATS, never full crawls. One live smoke test exists
behind a build tag:

```
CONNIE_CONTACT="you@example.com" go test -tags live ./ats/smoke -v
```

## Etiquette

This scrapes public, unauthenticated APIs that ATS vendors provide so
postings can be shared. Be worthy of that: set `CONNIE_CONTACT`, keep
`--concurrency` at the default 8, and don't run discovery in a loop.

## Lineage

The Ashby scan semantics — the fuzzy-match rules, the seed/cache split, the
closed-at lifecycle — are ported from
[mherzog4/ashbyhq-jobs](https://github.com/mherzog4/ashbyhq-jobs), a
single-file Python tool whose README documents six behaviors that look like
bugs and are load-bearing. They survived the rewrite; so did the tests.

MIT © Joel Martinez · [pushtomain.xyz](https://pushtomain.xyz)

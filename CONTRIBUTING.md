# Contributing to connie

Thanks for helping. Connie aims to stay small and focused: find matching
jobs on public ATS boards, fast, and remember what it has seen. Fixes,
better parsing, more seed boards, docs, and tests are very welcome. For a
new feature or a new ATS, please open an issue first so we can agree on
scope before you write code.

## Setup

You need Go 1.26 or newer.

```sh
git clone https://github.com/joelrmartinez/connie
cd connie
make check     # gofmt, go vet, go test -race ./...
make build     # writes ./connie
```

No `make`? Run the same commands by hand:

```sh
gofmt -l .        # should print nothing
go vet ./...
go test -race ./...
```

## Ground rules

- All tests run offline against small recorded fixtures in `ats/*/testdata`.
  Never commit full crawls, your own scan output (`*.csv`, `*.json` in the
  repo root, `connie.db`), or a `boards/` cache.
- The live smoke test is opt-in and must have a real contact address:
  `CONNIE_CONTACT="you@example.com" go test -tags live ./ats/smoke -v`
- Be polite to the ATS APIs. Do not raise default concurrency, remove the
  per-host cap, or drop the User-Agent contact.
- Keep the binary static (`CGO_ENABLED=0`). Avoid new dependencies unless
  there is a strong reason.
- Run `go mod tidy` if you touch imports. CI checks it.

## Adding a seed board

Edit `seeds/<ats>.json` and add the company's board slug (the part of the
URL after the host, for example `openai` in `jobs.ashbyhq.com/openai`).
Check that the board loads and has open postings. Keep the list sorted.

## Adding or changing an ATS adapter

Adapters live in `ats/<name>/` and implement the interfaces in `ats/ats.go`.
Each needs a trimmed real API response in `testdata/` and tests for the
contract (parse, pay, 404 handling, and discovery if supported).

## Pull requests

- One focused change per PR, with tests.
- CI must pass (format, vet, race tests, tidy, govulncheck, macOS build).
- Describe what changed and how you tested it.

By contributing you agree your work is released under the MIT License and
that you will follow the [Code of Conduct](CODE_OF_CONDUCT.md).

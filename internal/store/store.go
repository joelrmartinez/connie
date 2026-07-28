// Package store is the Registry: a SQLite archive of every posting connie
// has ever seen, keyed "ats:board:external_id", with first_seen/last_seen
// history and a closed_at lifecycle.
//
// The lifecycle rules are deliberate and pinned by tests:
//   - only an unfiltered run may close postings (a filtered run not seeing a
//     job proves nothing);
//   - closing is scoped to (ats, board) pairs the run actually covered;
//   - a posting that reappears is reopened;
//   - grep context (matched) from an earlier run survives runs without grep.
package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver: CGO_ENABLED=0, static binary

	"github.com/saiwrd/connie/internal/ats"
)

const schema = `
CREATE TABLE IF NOT EXISTS jobs (
    key             TEXT PRIMARY KEY,
    ats             TEXT NOT NULL,
    board           TEXT NOT NULL,
    external_id     TEXT NOT NULL,
    title           TEXT NOT NULL DEFAULT '',
    location        TEXT NOT NULL DEFAULT '',
    department      TEXT NOT NULL DEFAULT '',
    team            TEXT NOT NULL DEFAULT '',
    employment_type TEXT NOT NULL DEFAULT '',
    remote          INTEGER NOT NULL DEFAULT 0,
    url             TEXT NOT NULL DEFAULT '',
    published_at    TEXT NOT NULL DEFAULT '',
    matched         TEXT NOT NULL DEFAULT '',
    first_seen      TEXT NOT NULL,
    last_seen       TEXT NOT NULL,
    closed_at       TEXT
);
CREATE INDEX IF NOT EXISTS jobs_board     ON jobs(ats, board);
CREATE INDEX IF NOT EXISTS jobs_last_seen ON jobs(last_seen);
CREATE INDEX IF NOT EXISTS jobs_closed_at ON jobs(closed_at);

CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL);
INSERT INTO schema_version (version)
    SELECT 1 WHERE NOT EXISTS (SELECT 1 FROM schema_version);
`

// Store is an open Registry.
type Store struct {
	db *sql.DB
}

// Row is a posting plus its grep context, as written by a scan.
type Row struct {
	ats.Job
	Matched string
}

// StoredRow is a posting as read back, with its Registry history.
type StoredRow struct {
	ats.Job
	Matched   string
	FirstSeen time.Time
	LastSeen  time.Time
	ClosedAt  *time.Time
}

// BoardRef names one (ats, board) pair a scan covered exhaustively.
type BoardRef struct {
	ATS   string
	Board string
}

// Result summarizes one Upsert.
type Result struct {
	New     int
	Updated int
	Closed  int
	NewKeys []string // keys first seen this run — the watch-mode feed
}

// QueryOptions narrows a Query.
type QueryOptions struct {
	OpenOnly       bool      // exclude closed postings
	FirstSeenAfter time.Time // only postings first seen strictly after this
}

// Open opens (creating if needed) the Registry at path and applies the schema.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open registry %q: %w", path, err)
	}
	// SQLite writes are serialized; a single connection avoids lock churn.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL on %q: %w", path, err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema to %q: %w", path, err)
	}
	return &Store{db: db}, nil
}

// Close releases the underlying database.
func (s *Store) Close() error { return s.db.Close() }

const upsertSQL = `
INSERT INTO jobs (key, ats, board, external_id, title, location, department,
                  team, employment_type, remote, url, published_at, matched,
                  first_seen, last_seen, closed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)
ON CONFLICT(key) DO UPDATE SET
    title           = excluded.title,
    location        = excluded.location,
    department      = excluded.department,
    team            = excluded.team,
    employment_type = excluded.employment_type,
    remote          = excluded.remote,
    url             = excluded.url,
    published_at    = excluded.published_at,
    matched         = CASE WHEN excluded.matched != ''
                           THEN excluded.matched ELSE jobs.matched END,
    last_seen       = excluded.last_seen,
    closed_at       = NULL
`

// Upsert writes rows in one transaction stamped seenAt. covered lists the
// (ats, board) pairs this run scanned exhaustively — pass nil on filtered
// runs, which then close nothing.
func (s *Store) Upsert(rows []Row, seenAt time.Time, covered []BoardRef) (Result, error) {
	var res Result
	stamp := seenAt.UTC().Format(time.RFC3339)

	tx, err := s.db.Begin()
	if err != nil {
		return res, fmt.Errorf("begin upsert: %w", err)
	}
	defer tx.Rollback()

	exists, err := tx.Prepare("SELECT 1 FROM jobs WHERE key = ?")
	if err != nil {
		return res, fmt.Errorf("prepare existence check: %w", err)
	}
	insert, err := tx.Prepare(upsertSQL)
	if err != nil {
		return res, fmt.Errorf("prepare upsert: %w", err)
	}

	for _, r := range rows {
		key := r.Key()
		var one int
		switch err := exists.QueryRow(key).Scan(&one); err {
		case nil:
			res.Updated++
		case sql.ErrNoRows:
			res.New++
			res.NewKeys = append(res.NewKeys, key)
		default:
			return Result{}, fmt.Errorf("check key %q: %w", key, err)
		}

		published := ""
		if !r.PublishedAt.IsZero() {
			published = r.PublishedAt.UTC().Format(time.RFC3339)
		}
		if _, err := insert.Exec(key, r.ATS, r.Board, r.ExternalID, r.Title,
			r.Location, r.Department, r.Team, r.EmploymentType, r.Remote,
			r.URL, published, r.Matched, stamp, stamp); err != nil {
			return Result{}, fmt.Errorf("upsert key %q: %w", key, err)
		}
	}

	// Close pass: vanished postings on covered boards only.
	if len(covered) > 0 {
		closeStmt, err := tx.Prepare(`
            UPDATE jobs SET closed_at = ?
            WHERE closed_at IS NULL AND last_seen < ? AND ats = ? AND board = ?`)
		if err != nil {
			return Result{}, fmt.Errorf("prepare close pass: %w", err)
		}
		for _, c := range covered {
			r, err := closeStmt.Exec(stamp, stamp, c.ATS, c.Board)
			if err != nil {
				return Result{}, fmt.Errorf("close pass %s/%s: %w", c.ATS, c.Board, err)
			}
			n, err := r.RowsAffected()
			if err != nil {
				return Result{}, fmt.Errorf("close pass count: %w", err)
			}
			res.Closed += int(n)
		}
	}

	if err := tx.Commit(); err != nil {
		return Result{}, fmt.Errorf("commit upsert: %w", err)
	}
	return res, nil
}

// Query reads rows matching opts, newest-first by first_seen.
func (s *Store) Query(opts QueryOptions) ([]StoredRow, error) {
	q := `SELECT key, ats, board, external_id, title, location, department,
                 team, employment_type, remote, url, published_at, matched,
                 first_seen, last_seen, closed_at
          FROM jobs WHERE 1=1`
	var args []any
	if opts.OpenOnly {
		q += " AND closed_at IS NULL"
	}
	if !opts.FirstSeenAfter.IsZero() {
		q += " AND first_seen > ?"
		args = append(args, opts.FirstSeenAfter.UTC().Format(time.RFC3339))
	}
	q += " ORDER BY first_seen DESC, key"

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("query registry: %w", err)
	}
	defer rows.Close()

	var out []StoredRow
	for rows.Next() {
		var (
			r         StoredRow
			key       string
			published string
			first     string
			last      string
			closed    sql.NullString
		)
		if err := rows.Scan(&key, &r.ATS, &r.Board, &r.ExternalID, &r.Title,
			&r.Location, &r.Department, &r.Team, &r.EmploymentType, &r.Remote,
			&r.URL, &published, &r.Matched, &first, &last, &closed); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		if published != "" {
			if t, err := time.Parse(time.RFC3339, published); err == nil {
				r.PublishedAt = t
			}
		}
		if t, err := time.Parse(time.RFC3339, first); err == nil {
			r.FirstSeen = t
		}
		if t, err := time.Parse(time.RFC3339, last); err == nil {
			r.LastSeen = t
		}
		if closed.Valid {
			if t, err := time.Parse(time.RFC3339, closed.String); err == nil {
				r.ClosedAt = &t
			}
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}
	return out, nil
}

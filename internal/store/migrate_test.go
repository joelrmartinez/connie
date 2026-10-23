package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/joelrmartinez/connie/ats"
)

// A Registry written before pay existed: the v1 table, one row.
const v1Fixture = `
CREATE TABLE jobs (
    key TEXT PRIMARY KEY, ats TEXT NOT NULL, board TEXT NOT NULL,
    external_id TEXT NOT NULL, title TEXT NOT NULL DEFAULT '',
    location TEXT NOT NULL DEFAULT '', department TEXT NOT NULL DEFAULT '',
    team TEXT NOT NULL DEFAULT '', employment_type TEXT NOT NULL DEFAULT '',
    remote INTEGER NOT NULL DEFAULT 0, url TEXT NOT NULL DEFAULT '',
    published_at TEXT NOT NULL DEFAULT '', matched TEXT NOT NULL DEFAULT '',
    first_seen TEXT NOT NULL, last_seen TEXT NOT NULL, closed_at TEXT);
CREATE TABLE schema_version (version INTEGER NOT NULL);
INSERT INTO schema_version VALUES (1);
INSERT INTO jobs (key, ats, board, external_id, title, first_seen, last_seen)
VALUES ('ashby:acme:1', 'ashby', 'acme', '1', 'GTM Engineer',
        '2026-07-01T00:00:00Z', '2026-07-01T00:00:00Z');
`

func TestMigrateV1Registry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(v1Fixture); err != nil {
		t.Fatalf("write v1 fixture: %v", err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open v1 registry: %v", err)
	}
	defer s.Close()

	var v int
	if err := s.db.QueryRow("SELECT version FROM schema_version").Scan(&v); err != nil || v != SchemaVersion {
		t.Fatalf("version = %d (%v), want %d", v, err, SchemaVersion)
	}
	old := get(t, s, "ashby:acme:1")
	if old.Title != "GTM Engineer" || old.Pay.Known() {
		t.Fatalf("migrated row = %+v; history kept, pay unknown", old)
	}

	// Reopening a migrated file is a no-op.
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	s2.Close()
}

func TestPayRoundTripAndSurvival(t *testing.T) {
	s := testStore(t)
	paid := job("acme", "1", "GTM Engineer")
	paid.Pay = ats.Pay{Min: 150000, Max: 180000, Currency: "USD", Period: ats.PerYear, Source: ats.PayFromDescription}
	mustUpsert(t, s, []Row{paid, job("acme", "2", "Growth Engineer")}, run1, nil)

	if got := get(t, s, paid.Key()).Pay; got != paid.Pay {
		t.Fatalf("pay = %+v, want %+v", got, paid.Pay)
	}
	if get(t, s, "ashby:acme:2").Pay.Known() {
		t.Fatal("unknown pay must read back unknown")
	}

	// A later run that found no pay (say, without description parsing)
	// keeps what was known.
	mustUpsert(t, s, []Row{job("acme", "1", "GTM Engineer")}, run2, nil)
	if got := get(t, s, paid.Key()).Pay; got != paid.Pay {
		t.Fatalf("pay lost on a run without pay: %+v", got)
	}

	// New known pay replaces old.
	field := job("acme", "1", "GTM Engineer")
	field.Pay = ats.Pay{Min: 160000, Max: 190000, Currency: "USD", Period: ats.PerYear, Source: ats.PayFromField}
	mustUpsert(t, s, []Row{field}, run3, nil)
	if got := get(t, s, paid.Key()).Pay; got != field.Pay {
		t.Fatalf("pay = %+v, want %+v", got, field.Pay)
	}
}

// Package store is the SQLite-backed, event-sourced archive for waveforms,
// properties, replay verdicts, adjudications and counterexamples.
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// SchemaVersion is bumped for each real migration; state migration tests replay
// every upgrade on a database built at the previous version.
const SchemaVersion = 4

func Open(dsn string) (*DB, error) {
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec("PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000;"); err != nil {
		conn.Close()
		return nil, err
	}
	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, err
	}
	return db, nil
}

type DB struct {
	conn *sql.DB
}

func (d *DB) Conn() *sql.Conn { return nil }
func (d *DB) Raw() *sql.DB    { return d.conn }
func (d *DB) Close() error    { return d.conn.Close() }

// migrations is an explicit, append-only upgrade chain. Each migration runs in
// its own transaction and records the version it produced.
var migrations = []struct {
	version int
	up      string
}{
	{
		version: 1,
		up: `
CREATE TABLE meta(key TEXT PRIMARY KEY, value TEXT NOT NULL);

CREATE TABLE cases(
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	created_at INTEGER NOT NULL
);

CREATE TABLE sources(
	case_id TEXT NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
	kind TEXT NOT NULL,            -- vcd | clockdomains | properties
	raw BLOB NOT NULL,             -- original bytes, never rewritten
	sha256 TEXT NOT NULL,
	PRIMARY KEY(case_id, kind)
);

CREATE TABLE timescale(
	case_id TEXT PRIMARY KEY REFERENCES cases(id) ON DELETE CASCADE,
	raw TEXT NOT NULL, number INTEGER NOT NULL, unit TEXT NOT NULL,
	base_fs INTEGER NOT NULL, convertible INTEGER NOT NULL
);

CREATE TABLE signals(
	id TEXT PRIMARY KEY, case_id TEXT NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
	code TEXT NOT NULL, name TEXT NOT NULL, raw_name TEXT NOT NULL, width INTEGER NOT NULL
);

CREATE TABLE changes(
	id TEXT PRIMARY KEY, case_id TEXT NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
	signal_id TEXT NOT NULL, seq INTEGER NOT NULL,
	at_tick INTEGER NOT NULL, raw_value TEXT NOT NULL,
	bits TEXT NOT NULL, width INTEGER NOT NULL, non_canonical INTEGER NOT NULL
);

CREATE TABLE clock_domains(
	id TEXT PRIMARY KEY, case_id TEXT NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
	name TEXT NOT NULL, clock_signal TEXT NOT NULL, edge TEXT NOT NULL,
	reset_signal TEXT NOT NULL, reset_level INTEGER NOT NULL, raw_text TEXT NOT NULL
);

CREATE TABLE properties(
	id TEXT NOT NULL, case_id TEXT NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
	branch_id TEXT NOT NULL,
	name TEXT NOT NULL, raw_text TEXT NOT NULL, ast_json TEXT NOT NULL,
	clock_domain TEXT NOT NULL, disable_signal TEXT NOT NULL,
	disable_level INTEGER NOT NULL, sem_ver TEXT NOT NULL,
	PRIMARY KEY(id, branch_id)
);

CREATE TABLE branches(
	id TEXT PRIMARY KEY, case_id TEXT NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
	name TEXT NOT NULL, parent_id TEXT NOT NULL, created_by TEXT NOT NULL,
	version INTEGER NOT NULL, head_event TEXT NOT NULL, is_current INTEGER NOT NULL
);

CREATE TABLE events(
	seq INTEGER PRIMARY KEY AUTOINCREMENT,
	id TEXT NOT NULL UNIQUE,
	case_id TEXT NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
	branch_id TEXT NOT NULL,
	kind TEXT NOT NULL,
	parent_event TEXT NOT NULL,
	refs_json TEXT NOT NULL,
	deps_json TEXT NOT NULL,
	detail_json TEXT NOT NULL,
	version INTEGER NOT NULL
);

CREATE TABLE adjudications(
	id TEXT PRIMARY KEY,
	case_id TEXT NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
	branch_id TEXT NOT NULL,
	target_id TEXT NOT NULL,
	decision TEXT NOT NULL,
	reason TEXT NOT NULL,
	undone INTEGER NOT NULL DEFAULT 0,
	supersedes TEXT NOT NULL DEFAULT '',
	event_id TEXT NOT NULL,
	version INTEGER NOT NULL
);

CREATE TABLE verdicts(
	id TEXT NOT NULL,
	case_id TEXT NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
	branch_id TEXT NOT NULL,
	property_id TEXT NOT NULL,
	sem_ver TEXT NOT NULL,
	result TEXT NOT NULL,
	empty_match INTEGER NOT NULL,
	candidates_json TEXT NOT NULL,
	threads_json TEXT NOT NULL,
	stale INTEGER NOT NULL DEFAULT 0,
	event_id TEXT NOT NULL,
	PRIMARY KEY(id, branch_id)
);

CREATE TABLE counterexamples(
	id TEXT PRIMARY KEY,
	case_id TEXT NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
	branch_id TEXT NOT NULL,
	verdict_id TEXT NOT NULL,
	origin_sample INTEGER NOT NULL, origin_tick INTEGER NOT NULL,
	kept_json TEXT NOT NULL, removed_json TEXT NOT NULL,
	minimal INTEGER NOT NULL, alternatives_json TEXT NOT NULL,
	stale INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE deps(
	from_ref TEXT NOT NULL,           -- derived object id
	to_ref TEXT NOT NULL,             -- source/param/decision it depends on
	case_id TEXT NOT NULL,
	PRIMARY KEY(from_ref, to_ref)
);

CREATE TABLE diagnostics(
	id TEXT PRIMARY KEY,
	case_id TEXT NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
	ref_id TEXT NOT NULL, code TEXT NOT NULL,
	message TEXT NOT NULL, raw_value TEXT NOT NULL
);
`,
	},
	{
		// Real schema change used by the state migration tests.
		version: 2,
		up:      `ALTER TABLE events ADD COLUMN origin_ref TEXT NOT NULL DEFAULT '';`,
	},
	{
		// Second migration: adjudication provenance actor label.
		version: 3,
		up:      `ALTER TABLE events ADD COLUMN actor_origin TEXT NOT NULL DEFAULT '';`,
	},
	{
		// Branch-local source overrides: a localized source change never
		// rewrites the parent's raw bytes.
		version: 4,
		up: `CREATE TABLE branch_sources(
			case_id TEXT NOT NULL,
			branch_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			raw BLOB NOT NULL,
			sha256 TEXT NOT NULL,
			PRIMARY KEY(case_id, branch_id, kind)
		);`,
	},
}

func (d *DB) migrate() error {
	var cur int
	row := d.conn.QueryRow("SELECT user_version FROM pragma_user_version")
	if err := row.Scan(&cur); err != nil {
		return err
	}
	for _, m := range migrations {
		if m.version <= cur {
			continue
		}
		tx, err := d.conn.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(m.up); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", m.version, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		cur = m.version
	}
	return nil
}

func (d *DB) UserVersion() int {
	var v int
	d.conn.QueryRow("SELECT user_version FROM pragma_user_version").Scan(&v)
	return v
}
